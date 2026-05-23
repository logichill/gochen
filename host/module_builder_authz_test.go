package host

import (
	"testing"

	auth "gochen/auth"
)

type builderAuthzTarget struct{}

func TestNewModuleDescriptor_CapturesAuthzMetadata(t *testing.T) {
	desc, err := newModuleDescriptorForTest(t,
		Module("demo").
			Name("Demo").
			Permission("demo.read").
			PermissionDefinitions(
				auth.PermissionDefinition{
					Code:        "demo.write",
					Name:        "Write Demo",
					Description: "allow write demo data",
					Scopes:      []string{"tenant"},
				},
			).
			ResourceResolver(
				auth.TypedResourceResolver[*builderAuthzTarget](func(target *builderAuthzTarget) (auth.Resource, bool) {
					return auth.Resource{Kind: "demo.target"}, target != nil
				}),
			),
	)
	if err != nil {
		t.Fatalf("NewModuleDescriptor failed: %v", err)
	}

	if len(desc.Permissions) != 2 || desc.Permissions[0] != "demo.read" || desc.Permissions[1] != "demo.write" {
		t.Fatalf("unexpected permissions: %#v", desc.Permissions)
	}
	if got := findBuilderPermissionDefinition(desc.PermissionDefinitions, "demo.read"); got.Code != "demo.read" {
		t.Fatalf("expected bare definition for demo.read, got %#v", got)
	}
	if got := findBuilderPermissionDefinition(desc.PermissionDefinitions, "demo.write"); got.Name != "Write Demo" || got.Description != "allow write demo data" || len(got.Scopes) != 1 || got.Scopes[0] != "tenant" {
		t.Fatalf("unexpected definition for demo.write: %#v", got)
	}
	if len(desc.ResourceResolvers) != 1 {
		t.Fatalf("expected 1 resource resolver, got %#v", desc.ResourceResolvers)
	}
}

func findBuilderPermissionDefinition(definitions []auth.PermissionDefinition, code string) auth.PermissionDefinition {
	for _, definition := range definitions {
		if definition.Code == code {
			return definition
		}
	}
	return auth.PermissionDefinition{}
}
