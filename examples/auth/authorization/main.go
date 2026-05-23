package main

import (
	"context"
	"log"

	"gochen/auth"
)

var documentPermissions = auth.NewAPIPermissionSet(
	"document",
	auth.PermissionActionRead,
	auth.PermissionActionWrite,
)

type document struct {
	ID             string
	TenantID       string
	ManagedScopeID int64
}

func main() {
	log.SetFlags(0)

	readPermission := documentPermissions.Code(auth.PermissionActionRead)
	writePermission := documentPermissions.Code(auth.PermissionActionWrite)
	target := &document{ID: "doc-1", TenantID: "tenant-a", ManagedScopeID: 1001}

	authorizer := mustAuthorizer()
	ctx := mustContext(auth.WithPrincipal(context.Background(), auth.Principal{
		SubjectID:     42,
		ActiveScopeID: target.ManagedScopeID,
		Permissions:   []string{readPermission},
	}))

	readDecision := mustDecision(authorizer.Authorize(ctx, readPermission, target))
	log.Printf("read: effect=%s resources=%d", readDecision.Effect, len(readDecision.AuthorizedResources))

	writeDecision := mustDecision(authorizer.Authorize(ctx, writePermission, target))
	log.Printf("write: effect=%s reason=%s", writeDecision.Effect, writeDecision.ReasonCode)

	if err := writeDecision.RequireAllow(); err != nil {
		log.Printf("write rejected: %v", err)
	}
}

func mustAuthorizer() auth.IAuthorizer {
	resolver := auth.TypedResourceResolver[*document](func(doc *document) (auth.Resource, bool) {
		if doc == nil {
			return auth.Resource{}, false
		}
		return auth.Resource{
			Kind:           "document",
			ID:             doc.ID,
			TenantID:       doc.TenantID,
			ManagedScopeID: doc.ManagedScopeID,
		}, true
	})

	authorizer, err := auth.NewAuthorizer(
		auth.EvaluatorFunc(func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
			if principal.AllowsPermission(permission) {
				return auth.AllowDecision(resources...), nil
			}
			return auth.DenyDecision("permission_not_granted", resources...), nil
		}),
		resolver,
	)
	if err != nil {
		log.Fatal(err)
	}
	return authorizer
}

func mustContext(ctx context.Context, err error) context.Context {
	if err != nil {
		log.Fatal(err)
	}
	return ctx
}

func mustDecision(decision auth.AuthzDecision, err error) auth.AuthzDecision {
	if err != nil {
		log.Fatal(err)
	}
	return decision
}
