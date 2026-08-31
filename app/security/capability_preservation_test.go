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
	"gochen/domain"
	"gochen/domain/audited"
	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
)

// 装饰器**可选能力保持**回归（审计发现的缺陷 2）。
//
// api/rest 在 Application 上做类型断言决定路由与装配：
//
//	appcrud.IBatchWriter          → 批量路由
//	audited 能力面                 → purge / restore / 审计轨迹路由
//	IServiceConfigUpdatable 等     → 装配期 config / validator / hooks 注入
//
// 装饰器一旦只实现 IApplication 本体，这些断言会全部失败：批量与审计路由
// 静默消失，装配期注入直接报错。此前的测试完全没有覆盖"装饰器 + 能力探测"
// 这个组合，所以缺陷在全绿状态下潜伏。

type capOrder struct {
	domcrud.Entity[int64]
	Name string `json:"name"`
}

func (o *capOrder) Validate() error { return nil }

// restAuditedSurface 复刻 api/rest.IAuditedService 的方法集（含 Delete）。
//
// 刻意在这里重新声明而不是 import：gochen 不依赖 runtime，
// 而 Go 的接口断言是结构化的——本地声明能等价地验证 REST 侧探测会不会成功。
type restAuditedSurface[T any, ID comparable] interface {
	Delete(ctx context.Context, id ID) error
	Purge(ctx context.Context, id ID) error
	Restore(ctx context.Context, id ID, by string) error
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error)
	AuditStore() audited.IAuditStore
}

type assemblyAware[T domain.IEntity[ID], ID comparable] interface {
	appcrud.IServiceConfigUpdatable
	appcrud.IValidatorAware
	appcrud.IHooksAware[T, ID]
}

func newCapApp(t *testing.T, scoped bool) appcrud.IApplication[*capOrder, int64] {
	t.Helper()
	opts := []testkit.MemoryOption[*capOrder, int64]{}
	if scoped {
		opts = append(opts, testkit.WithMemoryScope[*capOrder, int64]("order"))
	}
	repo := testkit.NewMemoryRepository[*capOrder, int64](testkit.NewInt64Sequence(1), opts...)
	app, err := appcrud.NewApplication[*capOrder, int64](repo, nil, nil)
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

// L1 装饰后，批量写与装配期注入能力必须仍可被探测到。
func TestActionDecoratorPreservesOptionalCapabilities(t *testing.T) {
	inner := newCapApp(t, false)
	secured, err := secaction.New[*capOrder, int64](inner, &allowChecker{}, capPolicy())
	require.NoError(t, err)

	_, ok := any(secured).(appcrud.IBatchWriter[*capOrder, int64])
	require.True(t, ok)

	_, ok = any(secured).(assemblyAware[*capOrder, int64])
	require.True(t, ok)
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

	_, ok = any(secured).(assemblyAware[*capOrder, int64])
	require.True(t, ok)
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
