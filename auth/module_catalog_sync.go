package auth

import (
	"gochen/errors"
	"sort"
	"sync"
)

// ModuleCatalogSyncFunc 定义模块 authz 目录同步钩子。
type ModuleCatalogSyncFunc func(ModuleRegistration) error

var moduleCatalogSyncRegistry struct {
	mu      sync.RWMutex
	hooks   []ModuleCatalogSyncFunc
	modules map[string]moduleCatalogSnapshot
}

type moduleCatalogSnapshot struct {
	registration ModuleRegistration
	revision     uint64
}

func cloneModuleCatalogRegistrations(registrations map[string]moduleCatalogSnapshot) []moduleCatalogSnapshot {
	if len(registrations) == 0 {
		return nil
	}

	moduleIDs := make([]string, 0, len(registrations))
	for moduleID := range registrations {
		moduleIDs = append(moduleIDs, moduleID)
	}
	sort.Strings(moduleIDs)

	out := make([]moduleCatalogSnapshot, 0, len(moduleIDs))
	for _, moduleID := range moduleIDs {
		snapshot := registrations[moduleID]
		out = append(out, moduleCatalogSnapshot{
			registration: cloneModuleRegistration(snapshot.registration),
			revision:     snapshot.revision,
		})
	}
	return out
}

// InstallModuleCatalogSync 安装模块 authz 目录同步钩子。
func InstallModuleCatalogSync(fn ModuleCatalogSyncFunc) error {
	moduleCatalogSyncRegistry.mu.Lock()
	if fn == nil {
		moduleCatalogSyncRegistry.hooks = nil
		moduleCatalogSyncRegistry.modules = nil
		moduleCatalogSyncRegistry.mu.Unlock()
		return nil
	}
	moduleCatalogSyncRegistry.hooks = append(moduleCatalogSyncRegistry.hooks, fn)
	modules := cloneModuleCatalogRegistrations(moduleCatalogSyncRegistry.modules)
	moduleCatalogSyncRegistry.mu.Unlock()

	var errs []error
	for _, snapshot := range modules {
		if !moduleCatalogRevisionMatches(snapshot.registration.ModuleID, snapshot.revision) {
			continue
		}
		if err := fn(snapshot.registration); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func rememberModuleCatalog(reg ModuleRegistration) {
	if reg.ModuleID == "" {
		return
	}
	if moduleCatalogSyncRegistry.modules == nil {
		moduleCatalogSyncRegistry.modules = make(map[string]moduleCatalogSnapshot)
	}
	snapshot := moduleCatalogSyncRegistry.modules[reg.ModuleID]
	snapshot.registration = cloneModuleRegistration(reg)
	snapshot.revision++
	moduleCatalogSyncRegistry.modules[reg.ModuleID] = snapshot
}

func moduleCatalogRevisionMatches(moduleID string, revision uint64) bool {
	if moduleID == "" {
		return false
	}
	moduleCatalogSyncRegistry.mu.RLock()
	defer moduleCatalogSyncRegistry.mu.RUnlock()
	snapshot, ok := moduleCatalogSyncRegistry.modules[moduleID]
	if !ok {
		return false
	}
	return snapshot.revision == revision
}

// SyncModuleCatalog 将模块 authz 目录同步到额外的治理注册表。
func SyncModuleCatalog(reg ModuleRegistration) error {
	moduleCatalogSyncRegistry.mu.Lock()
	rememberModuleCatalog(reg)
	hooks := append([]ModuleCatalogSyncFunc(nil), moduleCatalogSyncRegistry.hooks...)
	moduleCatalogSyncRegistry.mu.Unlock()
	if len(hooks) == 0 {
		return nil
	}

	var errs []error
	for _, hook := range hooks {
		if err := hook(reg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
