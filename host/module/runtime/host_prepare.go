package runtime

import (
	"context"
	dibasic "gochen/di/basic"

	"gochen/errors"
	"gochen/host/internal/bootstrap"
)

// Prepare 准备服务依赖并完成初始化装配。
func (s *Host) Prepare(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	if s.container == nil {
		s.container = dibasic.New()
	}

	if err := s.ensureFrameworkRegistries(); err != nil {
		return errors.Wrap(err, errors.Dependency, "failed to initialize framework registries")
	}

	runtime, err := bootstrap.Prepare(bootstrap.Config{
		Container:                s.container,
		Host:                     s.config.Host,
		Port:                     s.config.Port,
		BasePath:                 s.config.BasePath,
		SecurityLayer:            s.config.SecurityLayer,
		AllowSession:             s.config.AllowSession,
		RouteMiddlewares:         s.config.RouteMiddlewares,
		DisableHealthRoute:       s.config.DisableHealthRoute,
		FailFastOnRouteConflicts: s.config.FailFastOnRouteConflicts,
		HTTPServer:               s.config.HTTPServer,
		EventBus:                 s.config.EventBus,
		Transport:                s.config.Transport,
		ProjectionManager:        s.config.ProjectionManager,
	})
	if err != nil {
		return errors.Wrap(err, errors.Dependency, "failed to prepare host runtime")
	}
	s.runtime = runtime

	// 1) 构建模块实例
	modules, err := BuildModules(s.ctors...)
	if err != nil {
		return err
	}
	s.modules = modules
	if err := s.ensureModuleIDs(); err != nil {
		return err
	}
	if err := s.normalizeModuleHTTPConfigKeys(); err != nil {
		return err
	}
	if err := s.sortModulesByDependencies(); err != nil {
		return err
	}
	if err := s.registerModules(); err != nil {
		return err
	}
	if err := s.registerModuleAuthz(); err != nil {
		return err
	}

	for _, m := range s.modules {
		if m == nil {
			continue
		}
		opts := s.makeModuleInitOptions(m)
		if err := m.Init(opts); err != nil {
			return errors.Wrap(err, errors.Dependency, "failed to init module").WithContext("module", m.Name())
		}
	}

	return nil
}
