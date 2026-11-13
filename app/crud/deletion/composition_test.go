package deletion

import (
	"context"
	"gochen/app"
	appcrud "gochen/app/crud"
	appaudit "gochen/app/crud/audit"
	"gochen/audit"
	"gochen/auth"
	"gochen/errors"
)

// testApplication 只在测试中组合三个独立能力以复用既有行为回归。
type testApplication[T IEntity[ID], ID comparable] struct {
	*appcrud.Application[T, ID]
	deletion *Application[T, ID]
	history  *appaudit.Reader[ID]
}
type testOptions[T IEntity[ID], ID comparable] struct {
	appcrud.Options[T, ID]
	Projector app.Projector[T]
}

func newTestApplication[T IEntity[ID], ID comparable](repo Repository[T, ID], store app.IAuditRepository, opts testOptions[T, ID]) (*testApplication[T, ID], error) {
	if store == nil {
		return nil, errors.NewCode(errors.InvalidInput, "test composition requires audit")
	}
	options := opts.Options
	options.Audit = store
	options.Projector = opts.Projector
	// 每个入口只声明自己的标准动作，业务自定义写动作属于 CRUD。
	resourceOptions := options
	resourceOptions.ActionPermissions = map[string]string{}
	deletionOptions := options
	deletionOptions.Actions = nil
	deletionOptions.ActionPermissions = map[string]string{}
	for key, value := range options.ActionPermissions {
		switch key {
		case "delete", "restore", "purge", "list_deleted":
			deletionOptions.ActionPermissions[key] = value
		case "audit_read":
		default:
			resourceOptions.ActionPermissions[key] = value
		}
	}
	resource, err := appcrud.NewApplication(repo, resourceOptions)
	if err != nil {
		return nil, err
	}
	trash, err := NewApplication(repo, Options[T, ID]{Options: deletionOptions})
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
	return &testApplication[T, ID]{Application: resource, deletion: trash, history: history}, nil
}
func (a *testApplication[T, ID]) Delete(ctx context.Context, id ID) error {
	return a.deletion.Delete(ctx, id)
}
func (a *testApplication[T, ID]) DeleteBulk(ctx context.Context, ids []ID) error {
	return a.deletion.DeleteBulk(ctx, ids)
}
func (a *testApplication[T, ID]) Restore(ctx context.Context, id ID, opts ...ExecOption) (T, error) {
	return a.deletion.Restore(ctx, id, opts...)
}
func (a *testApplication[T, ID]) Purge(ctx context.Context, id ID, opts ...ExecOption) error {
	return a.deletion.Purge(ctx, id, opts...)
}
func (a *testApplication[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	return a.deletion.ListDeleted(ctx, offset, limit)
}
func (a *testApplication[T, ID]) AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audit.AuditRecord, error) {
	return a.history.AuditTrail(ctx, id, offset, limit)
}
func (a *testApplication[T, ID]) Descriptor() auth.Descriptor {
	d := a.Application.Descriptor()
	d.Actions = append(d.Actions, a.deletion.Descriptor().Actions...)
	d.Actions = append(d.Actions, a.history.Descriptor().Actions...)
	return d
}
