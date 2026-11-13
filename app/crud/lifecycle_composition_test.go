package crud_test

import (
	"context"
	"gochen/app"
	appcrud "gochen/app/crud"
	appaudit "gochen/app/crud/audit"
	"gochen/audit"
	"gochen/auth"
	"gochen/domain"
	"gochen/errors"
)

// testApplication 只在测试中组合资源用例与独立历史读取。
type testApplication[T domain.IEntity[ID], ID comparable] struct {
	*appcrud.Application[T, ID]
	history *appaudit.Reader[ID]
}
type testOptions[T domain.IEntity[ID], ID comparable] struct {
	appcrud.Options[T, ID]
	Projector app.Projector[T]
}

func newTestApplication[T domain.IEntity[ID], ID comparable](repo appcrud.Repository[T, ID], store app.IAuditRepository, opts testOptions[T, ID]) (*testApplication[T, ID], error) {
	if store == nil {
		return nil, errors.NewCode(errors.InvalidInput, "test composition requires audit")
	}
	options := opts.Options
	options.Audit = store
	options.Projector = opts.Projector
	options.DeleteMode = appcrud.DeleteSoft
	options.EnableRestore = true
	options.EnableDeletedQuery = true
	resourceOptions := options
	resourceOptions.ActionPermissions = map[string]string{}
	for key, value := range options.ActionPermissions {
		if key != "audit_read" {
			resourceOptions.ActionPermissions[key] = value
		}
	}
	resource, err := appcrud.NewApplication(repo, resourceOptions)
	if err != nil {
		return nil, err
	}
	query, ok := store.(audit.IAuditQueryStore)
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "test composition requires audit query")
	}
	history, err := appaudit.NewReader[ID](query, repo, appaudit.Options{Resource: options.Resource, Authorizer: options.Authorizer, Limits: options.Limits, Permission: options.ActionPermissions["audit_read"]})
	if err != nil {
		return nil, err
	}
	return &testApplication[T, ID]{Application: resource, history: history}, nil
}
func (a *testApplication[T, ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audit.AuditRecord, error) {
	return a.history.AuditTrail(ctx, id, offset, limit)
}
func (a *testApplication[T, ID]) Descriptor() auth.Descriptor {
	d := a.Application.Descriptor()
	d.Actions = append(d.Actions, a.history.Descriptor().Actions...)
	return d
}
