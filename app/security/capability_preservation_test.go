package security_test

import (
	"context"
	"testing"

	"gochen/testkit/require"

	appcrud "gochen/app/crud"
	secaction "gochen/app/security/action"
	secscoped "gochen/app/security/scoped"
	authaction "gochen/auth/action"
	authscoped "gochen/auth/scoped"
	"gochen/domain/audited"
	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
)

type capOrder struct {
	domcrud.Entity[int64]
	Name string `json:"name"`
}

func (o *capOrder) Validate() error { return nil }

// restAuditedSurface 复刻 api/rest.IAuditedService 的方法集（含 Delete）。
//
// 特意不声明 AuditStore：P2-051 已从能力接口移除该旁路句柄，
// 这里用来验证装饰器不再向外泄露底层审计存储。
type restAuditedSurface[T any, ID comparable] interface {
	HasPurgeCapability() bool
	HasAuditStore() bool
	Delete(ctx context.Context, id ID) error
	Purge(ctx context.Context, id ID) error
	Restore(ctx context.Context, id ID, by string) error
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error)
}

// leakyAuditStoreSurface 声明旧的白盒句柄；装饰器不得再满足它。
type leakyAuditStoreSurface interface {
	AuditStore() audited.IAuditStore

	HasPurgeCapability() bool
	Purge(ctx context.Context, id int64) error
	Restore(ctx context.Context, id int64, by string) error
}

func newCapApp(t *testing.T, scoped bool) appcrud.IApplication[*capOrder, int64] {
	t.Helper()
	opts := []testkit.MemoryOption[*capOrder, int64]{}
	if scoped {
		opts = append(opts, testkit.WithMemoryScope[*capOrder, int64]("order"))
	}
	repo := testkit.NewMemoryRepository[*capOrder, int64](testkit.NewInt64Sequence(1), opts...)
	app, err := appcrud.NewApplication[*capOrder, int64](repo, nil)
	require.NoError(t, err)
	return app
}

func capPolicy() authaction.OperationPolicy {
	return authaction.OperationPolicy{
		Create:    "order:api:create",
		Read:      "order:api:read",
		Update:    "order:api:update",
		Delete:    "order:api:delete",
		List:      "order:api:list",
		Restore:   "order:api:restore",
		Purge:     "order:api:purge",
		AuditRead: "order:api:audit_read",
	}
}

type allowChecker struct{ seen []string }

func (c *allowChecker) RequireAction(_ context.Context, code string) error {
	c.seen = append(c.seen, code)
	return nil
}

type denyChecker struct{}

func (denyChecker) RequireAction(_ context.Context, code string) error {
	return errors.NewCode(errors.Forbidden, "denied").WithContext("action", code)
}

// L1 保留批量业务能力，不向外暴露装配仓储。
func TestActionDecoratorPreservesOptionalCapabilities(t *testing.T) {
	inner := newCapApp(t, false)
	secured, err := secaction.New[*capOrder, int64](inner, &allowChecker{}, capPolicy())
	require.NoError(t, err)

	_, ok := any(secured).(appcrud.IBatchWriter[*capOrder, int64])
	require.True(t, ok)

	_, ok = any(secured).(appcrud.IRepositoryProvider[*capOrder, int64])
	require.False(t, ok, "secured applications must not expose raw repositories")
}

// 批量入口不是旁路：它同样要过动作校验。
func TestActionDecoratorGuardsBatchWrites(t *testing.T) {
	inner := newCapApp(t, false)
	secured, err := secaction.New[*capOrder, int64](inner, denyChecker{}, capPolicy())
	require.NoError(t, err)

	batch, ok := any(secured).(appcrud.IBatchWriter[*capOrder, int64])
	require.True(t, ok)

	err = batch.CreateAll(context.Background(), []*capOrder{{Name: "n1"}})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 批量放行时动作码与单条一致。
func TestActionDecoratorBatchUsesSameActionCode(t *testing.T) {
	inner := newCapApp(t, false)
	checker := &allowChecker{}
	secured, err := secaction.New[*capOrder, int64](inner, checker, capPolicy())
	require.NoError(t, err)

	batch, ok := any(secured).(appcrud.IBatchWriter[*capOrder, int64])
	require.True(t, ok)
	require.NoError(t, batch.CreateAll(context.Background(), []*capOrder{{Name: "n1"}}))
	require.Equal(t, []string{"order:api:create"}, checker.seen)
}

// 内层没有审计能力时，外壳也不得假冒——探测必须如实失败。
func TestActionDecoratorDoesNotFakeAuditedCapability(t *testing.T) {
	inner := newCapApp(t, false)
	secured, err := secaction.New[*capOrder, int64](inner, &allowChecker{}, capPolicy())
	require.NoError(t, err)

	_, ok := any(secured).(restAuditedSurface[*capOrder, int64])
	require.False(t, ok)
}

// L3 装饰后同样保住可选能力。
func TestScopedDecoratorPreservesOptionalCapabilities(t *testing.T) {
	inner := newCapApp(t, true)
	secured, err := secscoped.New[*capOrder, int64](inner, allowAllAuthorizer(t), secscoped.Config{
		EntityType:    "order",
		Policy:        capPolicy(),
		ScopeResolver: authscoped.DataScopeResolverFunc(func(context.Context) (authscoped.DataScope, error) { return authscoped.Filtered(101), nil }),
	})
	require.NoError(t, err)

	_, ok := any(secured).(appcrud.IBatchWriter[*capOrder, int64])
	require.True(t, ok)

	_, ok = any(secured).(appcrud.IRepositoryProvider[*capOrder, int64])
	require.False(t, ok, "secured applications must not expose raw repositories")
}

// allowAllAuthorizer 一律放行，并把目标资源原样授权。
func allowAllAuthorizer(t *testing.T) authscoped.IAuthorizer {
	t.Helper()
	evaluator := authscoped.EvaluatorFunc(func(_ context.Context, _ string, resources []authscoped.Resource) (authscoped.Decision, error) {
		if len(resources) == 0 {
			return authscoped.Allow(authscoped.Resource{Kind: "order", ID: "*", ManagedScopeID: 101}), nil
		}
		return authscoped.Allow(resources...), nil
	})
	resolver := authscoped.ResourceResolverFunc(func(target any) (authscoped.Resource, bool) {
		o, ok := target.(*capOrder)
		if !ok {
			return authscoped.Resource{}, false
		}
		return authscoped.Resource{Kind: "order", ID: "1", ManagedScopeID: 101, Revision: "0"}, ok && o != nil
	})
	authorizer, err := authscoped.NewAuthorizer(resolver, evaluator)
	require.NoError(t, err)
	return authorizer
}

// capAuditedApp 在普通 CRUD 应用上补齐审计能力面（含旧的白盒句柄）。
type capAuditedApp struct {
	appcrud.IApplication[*capOrder, int64]
}

func (a *capAuditedApp) HasPurgeCapability() bool { return true }

func (a *capAuditedApp) HasAuditStore() bool { return true }

// AuditStore 故意保留旧白盒句柄：用于验证装饰器不会把它透传出去。
func (a *capAuditedApp) AuditStore() audited.IAuditStore { return nil }

func (a *capAuditedApp) ListDeleted(context.Context, int, int) ([]*capOrder, error) {
	return nil, nil
}

func (a *capAuditedApp) GetWithDeleted(ctx context.Context, id int64) (*capOrder, error) {
	return a.Get(ctx, id)
}

func (a *capAuditedApp) AuditTrail(context.Context, int64, int, int) ([]audited.AuditRecord, error) {
	return nil, nil
}

func (a *capAuditedApp) Restore(context.Context, int64, string) error { return nil }

func (a *capAuditedApp) Purge(context.Context, int64) error { return nil }

// P2-051 回归：装饰器必须透传 HasAuditStore，但不得再暴露 AuditStore 白盒句柄。
func TestAuditedDecoratorDoesNotLeakAuditStore(t *testing.T) {
	inner := &capAuditedApp{IApplication: newCapApp(t, false)}
	secured, err := secaction.New[*capOrder, int64](inner, &allowChecker{}, capPolicy())
	require.NoError(t, err)

	surface, ok := any(secured).(restAuditedSurface[*capOrder, int64])
	require.True(t, ok, "audited capability must still be exposed")
	require.True(t, surface.HasAuditStore())

	_, leaked := any(secured).(leakyAuditStoreSurface)
	require.False(t, leaked, "audited decorator must not expose raw AuditStore handle")
}

type narrowCapRepo struct{}

func (narrowCapRepo) Create(context.Context, *capOrder) error       { return nil }
func (narrowCapRepo) Get(context.Context, int64) (*capOrder, error) { return &capOrder{}, nil }
func (narrowCapRepo) Update(context.Context, *capOrder) error       { return nil }
func (narrowCapRepo) Delete(context.Context, int64) error           { return nil }
func (narrowCapRepo) HasScopeDeclaration() bool                     { return true }
func (narrowCapRepo) ResourceKind() string                          { return "order" }
func (narrowCapRepo) ValidateWriteConstraintSupport() error         { return nil }

func TestDecoratorsDoNotFakeMissingBatchOrQueryCapabilities(t *testing.T) {
	base, err := appcrud.NewApplication[*capOrder, int64](narrowCapRepo{}, nil)
	require.NoError(t, err)

	scopedSecured, err := secscoped.New[*capOrder, int64](base, allowAllAuthorizer(t), secscoped.Config{
		EntityType:    "order",
		Policy:        capPolicy(),
		ScopeResolver: authscoped.DataScopeResolverFunc(func(context.Context) (authscoped.DataScope, error) { return authscoped.Filtered(101), nil }),
	})
	require.NoError(t, err)

	_, hasBatch := any(scopedSecured).(appcrud.IBatchWriter[*capOrder, int64])
	require.False(t, hasBatch, "scoped decorator must not expose IBatchWriter when repo lacks batch capability")
	scopedCaps, ok := any(scopedSecured).(appcrud.ICapabilities)
	require.True(t, ok)
	require.False(t, scopedCaps.HasBatchCapability())
	require.False(t, scopedCaps.HasQueryCapability())
	require.False(t, scopedCaps.HasQueryableCapability())

	actionSecured, err := secaction.New[*capOrder, int64](scopedSecured, &allowChecker{}, capPolicy())
	require.NoError(t, err)

	_, hasBatch = any(actionSecured).(appcrud.IBatchWriter[*capOrder, int64])
	require.False(t, hasBatch, "action decorator must not expose IBatchWriter when repo lacks batch capability")
	actionCaps, ok := any(actionSecured).(appcrud.ICapabilities)
	require.True(t, ok)
	require.False(t, actionCaps.HasBatchCapability())
	require.False(t, actionCaps.HasQueryCapability())
	require.False(t, actionCaps.HasQueryableCapability())
}
