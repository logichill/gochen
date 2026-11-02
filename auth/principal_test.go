package auth_test

import (
	"context"
	"testing"

	"gochen/auth"
)

func TestPrincipalContext(t *testing.T) {
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: " u1 ", TenantID: " t1 ", ScopeID: 3})
	if err != nil {
		t.Fatalf("WithPrincipal: %v", err)
	}
	principal, err := auth.RequirePrincipal(ctx)
	if err != nil {
		t.Fatalf("RequirePrincipal: %v", err)
	}
	if principal.SubjectID != "u1" || principal.TenantID != "t1" || principal.ScopeID != 3 {
		t.Fatalf("principal = %+v", principal)
	}
	if _, err := auth.WithPrincipal(context.Background(), auth.Principal{}); err == nil {
		t.Fatal("expected missing subject error")
	}
	if _, err := auth.RequirePrincipal(context.Background()); err == nil {
		t.Fatal("expected unauthorized without principal")
	}
}

func TestAsSystem(t *testing.T) {
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "u1", TenantID: "t1", ScopeID: 3})
	if err != nil {
		t.Fatalf("WithPrincipal: %v", err)
	}
	systemCtx := auth.AsSystem(ctx, "order.approve")
	principal, err := auth.RequirePrincipal(systemCtx)
	if err != nil {
		t.Fatalf("RequirePrincipal: %v", err)
	}
	if !principal.System || principal.SubjectID != "u1" || principal.TenantID != "t1" {
		t.Fatalf("system principal = %+v", principal)
	}
	info, ok := auth.SystemInfoFromContext(systemCtx)
	if !ok || info.Initiator != "u1" || info.Reason != "order.approve" {
		t.Fatalf("system info = %+v, %v", info, ok)
	}

	// 原始 ctx 不受影响。
	original, _ := auth.RequirePrincipal(ctx)
	if original.System {
		t.Fatal("AsSystem must not mutate the original principal")
	}
}

func TestRebindingPrincipalClearsPreviousSystemInfo(t *testing.T) {
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	systemCtx := auth.AsSystem(ctx, "old execution")
	rebound, err := auth.WithPrincipal(systemCtx, auth.Principal{SubjectID: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if info, ok := auth.SystemInfoFromContext(rebound); ok {
		t.Fatalf("old system metadata survived identity rebinding: %+v", info)
	}
	if info, ok := auth.SystemInfoFromContext(systemCtx); !ok || info.Initiator != "alice" {
		t.Fatalf("original system context changed: %+v, %v", info, ok)
	}
}
