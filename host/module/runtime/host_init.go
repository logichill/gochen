package runtime

import (
	"strings"

	auth "gochen/auth"
	"gochen/di"
	"gochen/errors"
	"gochen/host/capability"
	"gochen/host/module"
	moduleasm "gochen/host/module/assembly"
	"gochen/host/module/runtimecap"
	"gochen/httpx"
)

func (s *Host) normalizeModuleHTTPConfigKeys() error {
	if s == nil || s.config == nil || len(s.config.ModuleHTTP) == 0 {
		return nil
	}
	normalized := make(map[string]ModuleHTTPConfig, len(s.config.ModuleHTTP))
	for rawID, cfg := range s.config.ModuleHTTP {
		id, err := normalizeModuleID(rawID)
		if err != nil {
			return errors.Wrap(err, errors.InvalidInput, "invalid module http config key").WithContext("raw_id", rawID)
		}
		if _, exists := normalized[id]; exists {
			return errors.NewCode(errors.Conflict, "duplicate module http config key").WithContext("id", id)
		}
		normalized[id] = cfg
	}
	s.config.ModuleHTTP = normalized
	return nil
}

// makeModuleInitOptions 构造模块初始化选项。
func (s *Host) makeModuleInitOptions(m IModule) ModuleInitOptions {
	var baseGroup httpx.IRouteGroup
	var eventBus capability.IEventSubscriber
	var pm capability.IProjectionManager
	var transport capability.ITransport
	if s.runtime != nil {
		baseGroup = s.runtime.BaseGroup
		eventBus = s.runtime.EventBus
		pm = s.runtime.ProjectionManager
		transport = s.runtime.Transport
	}
	httpOpt := s.makeHTTPOptions(m, baseGroup)
	var registry di.IRegistry
	var resolver di.IResolver
	var moduleContainer module.IModuleContainer
	if s.container != nil {
		registry = s.container
		resolver = s.container
		moduleContainer = s.container
	}
	opts := module.NewModuleInitOptions(
		registry,
		resolver,
		moduleContainer,
		runtimecap.HTTP(httpOpt),
		runtimecap.AuthzRegistry(s.authzRegistry),
		runtimecap.ModuleRegistry(s.moduleRegistry),
		runtimecap.MetadataRegistry(s.metadataRegistry),
		runtimecap.EventBus(eventBus),
		runtimecap.ProjectionManager(pm),
		runtimecap.Transport(transport),
	)
	return opts
}

func (s *Host) registerModules() error {
	if s == nil || s.moduleRegistry == nil {
		return nil
	}
	for _, m := range s.modules {
		if m == nil {
			continue
		}
		if err := s.moduleRegistry.Register(ModuleInfo{
			ID:   m.ID(),
			Name: m.Name(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Host) registerModuleAuthz() error {
	if s == nil || s.authzRegistry == nil {
		return nil
	}
	for _, m := range s.modules {
		if m == nil {
			continue
		}
		provider, ok := m.(moduleasm.IModuleAuthzProvider)
		if !ok {
			continue
		}
		reg := provider.AuthzRegistration()
		if id := s.moduleIDs[m]; id != "" {
			reg.ModuleID = id
		}
		if len(reg.Permissions) == 0 && len(reg.PermissionDefinitions) == 0 && len(reg.ResourceResolvers) == 0 {
			continue
		}
		if err := s.authzRegistry.RegisterModule(reg); err != nil {
			return err
		}
		snapshot, ok := s.authzRegistry.Module(reg.ModuleID)
		if !ok {
			return errors.NewCode(errors.Internal, "registered authz module snapshot not found").
				WithContext("module_id", reg.ModuleID)
		}
		if err := auth.SyncModuleCatalog(snapshot); err != nil {
			return err
		}
	}
	return nil
}

// makeHTTPOptions 构造HTTP选项。
func (s *Host) makeHTTPOptions(m IModule, baseGroup httpx.IRouteGroup) *runtimecap.ModuleHTTPOptions {
	if baseGroup == nil {
		return nil
	}

	id := ""
	if m != nil {
		if s != nil && s.moduleIDs != nil {
			id = s.moduleIDs[m]
		}
		if id == "" {
			id = strings.TrimSpace(m.ID())
		}
	}
	cfg, ok := s.config.ModuleHTTP[id]
	if ok && cfg.Disabled {
		return nil
	}

	prefix := ""
	if ok {
		prefix = cfg.Prefix
	} else if id != "" {
		prefix = "/" + id
	}
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	var mws []httpx.Middleware
	if ok && len(cfg.Middlewares) > 0 {
		mws = cfg.Middlewares
	}

	return runtimecap.NewModuleHTTPOptions(baseGroup, prefix, mws...)
}
