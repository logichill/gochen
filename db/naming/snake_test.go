package naming

import "testing"

func TestSnakeCaseHandlesInitialisms(t *testing.T) {
	tests := map[string]string{
		"ID":             "id",
		"OwnerID":        "owner_id",
		"TenantID":       "tenant_id",
		"ManagedScopeID": "managed_scope_id",
		"HTTPStatus":     "http_status",
		"OAuth2ID":       "oauth2_id",
	}

	for input, want := range tests {
		if got := SnakeCase(input); got != want {
			t.Fatalf("SnakeCase(%q) = %q, want %q", input, got, want)
		}
	}
}
