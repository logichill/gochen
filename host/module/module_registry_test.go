package module_test

import (
	"context"
	. "gochen/host/module"
	moduleasm "gochen/host/module/assembly"
	moduleruntime "gochen/host/module/runtime"
	"testing"

	auth "gochen/auth"
)

type registryTestModule struct {
	*BaseModule
	initFn func(ModuleInitOptions) error
}

func (m *registryTestModule) Init(opts ModuleInitOptions) error {
	if m.initFn != nil {
		return m.initFn(opts)
	}
	return nil
}

func TestModuleRegistry_Register_NormalizesAndLists(t *testing.T) {
	registry := NewModuleRegistry()
	if err := registry.Register(ModuleInfo{ID: " demo ", Name: "Demo"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := registry.Register(ModuleInfo{ID: "demo", Name: "Demo"}); err != nil {
		t.Fatalf("duplicate Register should be idempotent: %v", err)
	}

	modules := registry.Modules()
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].ID != "demo" || modules[0].Name != "Demo" {
		t.Fatalf("unexpected module snapshot: %#v", modules[0])
	}
}

func TestModuleRegistry_Register_RejectsConflictingName(t *testing.T) {
	registry := NewModuleRegistry()
	if err := registry.Register(ModuleInfo{ID: "demo", Name: "Demo"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := registry.Register(ModuleInfo{ID: "demo", Name: "Other"}); err == nil {
		t.Fatal("expected conflicting name to fail")
	}
}

func TestServerModuleRegistry_OnlyContainsExplicitRegistrations(t *testing.T) {
	t.Run("registers module identity from descriptor", func(t *testing.T) {
		registry := NewModuleRegistry()
		srv := moduleruntime.NewHost([]ModuleCtor{
			func() (IModule, error) {
				return &registryTestModule{
					BaseModule: NewBaseModule("demo", "Demo"),
				}, nil
			},
		}, moduleruntime.WithModuleRegistry(registry))

		if err := srv.Prepare(context.Background()); err != nil {
			t.Fatalf("Prepare failed: %v", err)
		}
		modules := registry.Modules()
		if len(modules) != 1 || modules[0].ID != "demo" || modules[0].Name != "Demo" {
			t.Fatalf("unexpected registry snapshot: %#v", modules)
		}
	})
}

func TestServerRegistersModuleAuthzFromDescriptor(t *testing.T) {
	if err := auth.InstallModuleCatalogSync(nil); err != nil {
		t.Fatalf("InstallModuleCatalogSync reset failed: %v", err)
	}
	t.Cleanup(func() {
		if err := auth.InstallModuleCatalogSync(nil); err != nil {
			t.Fatalf("InstallModuleCatalogSync cleanup failed: %v", err)
		}
	})

	registry := auth.NewRegistry()
	srv := moduleruntime.NewHost([]ModuleCtor{
		func() (IModule, error) {
			return moduleasm.NewModule(moduleasm.ModuleDescriptor{
				ID:   "demo",
				Name: "Demo",
				PermissionDefinitions: []auth.PermissionDefinition{
					{
						Code:        "demo.read",
						Name:        "Read Demo",
						Description: "allow read demo data",
						Scopes:      []string{"tenant"},
					},
				},
				ResourceResolvers: []auth.ITypedResourceResolver{
					auth.TypedResourceResolver[*registryTestAuthzTarget](func(target *registryTestAuthzTarget) (auth.Resource, bool) {
						return auth.Resource{Kind: "demo.target", ID: "1"}, target != nil
					}),
				},
			}), nil
		},
	}, moduleruntime.WithAuthzRegistry(registry))

	if err := srv.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	modules := registry.Modules()
	if len(modules) != 1 || modules[0].ModuleID != "demo" {
		t.Fatalf("unexpected authz modules: %#v", modules)
	}
	if got := findServerPermissionDefinition(modules[0].PermissionDefinitions, "demo.read"); got.Name != "Read Demo" || got.Description != "allow read demo data" || len(got.Scopes) != 1 || got.Scopes[0] != "tenant" {
		t.Fatalf("unexpected authz definition: %#v", got)
	}
	if permissions := registry.Permissions(); len(permissions) != 1 || permissions[0] != "demo.read" {
		t.Fatalf("unexpected authz permissions: %#v", permissions)
	}
	if _, ok := registry.Resolve(&registryTestAuthzTarget{}); !ok {
		t.Fatal("expected module authz resolver to be registered")
	}
}

type registryTestAuthzTarget struct{}

func TestServerSyncsModuleAuthzCatalogAfterRegistration(t *testing.T) {
	if err := auth.InstallModuleCatalogSync(nil); err != nil {
		t.Fatalf("InstallModuleCatalogSync reset failed: %v", err)
	}
	t.Cleanup(func() {
		if err := auth.InstallModuleCatalogSync(nil); err != nil {
			t.Fatalf("InstallModuleCatalogSync cleanup failed: %v", err)
		}
	})

	var synced auth.ModuleRegistration
	if err := auth.InstallModuleCatalogSync(func(reg auth.ModuleRegistration) error {
		synced = reg
		return nil
	}); err != nil {
		t.Fatalf("InstallModuleCatalogSync hook failed: %v", err)
	}

	srv := moduleruntime.NewHost([]ModuleCtor{
		func() (IModule, error) {
			return moduleasm.NewModule(moduleasm.ModuleDescriptor{
				ID:   "demo",
				Name: "Demo",
				PermissionDefinitions: []auth.PermissionDefinition{
					{
						Code:        "demo.read",
						Name:        "Read Demo",
						Description: "allow read demo data",
					},
					{
						Code:        "demo.write",
						Name:        "Write Demo",
						Description: "allow write demo data",
						Scopes:      []string{"tenant"},
					},
				},
			}), nil
		},
	})

	if err := srv.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if synced.ModuleID != "demo" || synced.ModuleName != "Demo" {
		t.Fatalf("unexpected synced module: %#v", synced)
	}
	if got := findServerPermissionDefinition(synced.PermissionDefinitions, "demo.read"); got.Name != "Read Demo" || got.Description != "allow read demo data" {
		t.Fatalf("unexpected synced read definition: %#v", got)
	}
	if got := findServerPermissionDefinition(synced.PermissionDefinitions, "demo.write"); got.Name != "Write Demo" || got.Description != "allow write demo data" || len(got.Scopes) != 1 || got.Scopes[0] != "tenant" {
		t.Fatalf("unexpected synced write definition: %#v", got)
	}
}

func TestServerSyncsNormalizedModuleAuthzCatalog(t *testing.T) {
	if err := auth.InstallModuleCatalogSync(nil); err != nil {
		t.Fatalf("InstallModuleCatalogSync reset failed: %v", err)
	}
	t.Cleanup(func() {
		if err := auth.InstallModuleCatalogSync(nil); err != nil {
			t.Fatalf("InstallModuleCatalogSync cleanup failed: %v", err)
		}
	})

	var synced auth.ModuleRegistration
	if err := auth.InstallModuleCatalogSync(func(reg auth.ModuleRegistration) error {
		synced = reg
		return nil
	}); err != nil {
		t.Fatalf("InstallModuleCatalogSync hook failed: %v", err)
	}

	registry := auth.NewRegistry()
	srv := moduleruntime.NewHost([]ModuleCtor{
		func() (IModule, error) {
			return moduleasm.NewModule(moduleasm.ModuleDescriptor{
				ID:   "/Demo",
				Name: "Demo",
				Permissions: []string{
					"demo.read",
				},
			}), nil
		},
	}, moduleruntime.WithAuthzRegistry(registry))

	if err := srv.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	modules := registry.Modules()
	if len(modules) != 1 || modules[0].ModuleID != "demo" {
		t.Fatalf("unexpected authz modules: %#v", modules)
	}
	if synced.ModuleID != "demo" || synced.ModuleName != "Demo" {
		t.Fatalf("unexpected synced module: %#v", synced)
	}
	if len(synced.PermissionDefinitions) != 1 {
		t.Fatalf("expected synced normalized definitions, got %#v", synced)
	}
	if got := synced.PermissionDefinitions[0]; got.Code != "demo.read" {
		t.Fatalf("unexpected synced permission definition: %#v", got)
	}
}

func findServerPermissionDefinition(definitions []auth.PermissionDefinition, code string) auth.PermissionDefinition {
	for _, definition := range definitions {
		if definition.Code == code {
			return definition
		}
	}
	return auth.PermissionDefinition{}
}
