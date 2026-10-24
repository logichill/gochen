package scoped_test

import (
	"context"
	"sync/atomic"
	"testing"

	secscoped "gochen/app/security/scoped"

	"gochen/app/crud"
	"gochen/auth/action"
	authscoped "gochen/auth/scoped"
	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
)

// degradedScopeRepo 故意不暴露资源边界探针（只嵌入仓储接口，不带
// ResolveResourceByID），迫使 scoped Get 走"预读实体再授权"的降级解析。
// 同时让 Get 消费 ctx 中的 DataScope（对齐 SQL 仓储的范围过滤语义），
// 用于验证 Get 返回值恒经 scopedCtx 二次读取、不被降级预读的快速路径绕过。
type degradedScopeRepo struct {
	domcrud.IRepository[*order, int64]

	inner       *testkit.MemoryRepository[*order, int64]
	scopedReads atomic.Int64
}

func newDegradedScopeRepo(t *testing.T) *degradedScopeRepo {
	t.Helper()
	inner := testkit.NewMemoryRepository[*order, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryScope[*order, int64]("order"),
	)
	return &degradedScopeRepo{IRepository: inner, inner: inner}
}

// 装配期探针按需转发给底层内存仓储。
func (r *degradedScopeRepo) HasScopeDeclaration() bool { return r.inner.HasScopeDeclaration() }

func (r *degradedScopeRepo) ResourceKind() string { return r.inner.ResourceKind() }

func (r *degradedScopeRepo) ValidateWriteConstraintSupport() error {
	return r.inner.ValidateWriteConstraintSupport()
}

// Get 消费 DataScope：目标实体的 ScopeID 不在范围内时报 NotFound，
// 与 SQL 仓储 `WHERE managed_scope_id IN (...)` 的过滤语义一致。
func (r *degradedScopeRepo) Get(ctx context.Context, id int64) (*order, error) {
	entity, err := r.inner.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope, ok := authscoped.DataScopeFromContext(ctx); ok {
		r.scopedReads.Add(1)
		if !scope.Contains(entity.ScopeID) {
			return nil, errors.NewCode(errors.NotFound, "record not found")
		}
	}
	return entity, nil
}

// 降级解析（无边界探针）模式下，Get 的返回值必须经绑定数据范围的
// scopedCtx 二次读取：PDP 资源级放行（此处 PDP 只看资源属性、判定范围
// 为全局）不等于主体数据范围覆盖目标实体。ScopeID 越出主体范围的实体
// 必须被范围过滤拦成 NotFound，而不是把预读实体直接返回。
func TestGetDegradedResolutionStillAppliesDataScope(t *testing.T) {
	repo := newDegradedScopeRepo(t)
	plain, err := crud.NewApplication[*order, int64](repo, nil)
	require.NoError(t, err)

	evaluator := authscoped.EvaluatorFunc(
		func(_ context.Context, _ string, resources []authscoped.Resource) (authscoped.Decision, error) {
			if len(resources) == 0 {
				return authscoped.Allow(authscoped.Resource{Kind: "order", ID: "*", GlobalScope: true}), nil
			}
			return authscoped.Allow(resources...), nil
		})
	resolver := authscoped.ResourceResolverFunc(func(target any) (authscoped.Resource, bool) {
		if _, ok := target.(*order); !ok {
			return authscoped.Resource{}, false
		}
		// PDP 不感知数据范围：资源按全局范围放行（模拟纯属性型策略/配置漂移）。
		// ID 留空 = 通配（Create 时实体尚未分配 ID）。
		return authscoped.Resource{Kind: "order", GlobalScope: true}, true
	})
	authorizer, err := authscoped.NewAuthorizer(resolver, evaluator)
	require.NoError(t, err)

	secured, err := secscoped.New[*order, int64](plain, authorizer, secscoped.Config{
		EntityType: "order",
		Policy: action.OperationPolicy{
			Create: "order:api:create",
			Read:   "order:api:read",
			List:   "order:api:list",
		},
		ScopeResolver: authscoped.DataScopeResolverFunc(
			func(context.Context) (authscoped.DataScope, error) {
				return authscoped.Filtered(101), nil
			}),
	})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, secured.Create(ctx, &order{ScopeID: 101, Name: "in-scope"}))
	require.NoError(t, secured.Create(ctx, &order{ScopeID: 202, Name: "out-of-scope"}))

	// 范围内实体可读。
	got, err := secured.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(101), got.ScopeID)

	// 范围外实体：PDP 放行，但 scoped 二次读取必须按范围过滤拦成 NotFound。
	_, err = secured.Get(ctx, 2)
	require.True(t, errors.Is(err, errors.NotFound),
		"out-of-scope entity must be filtered by the scoped re-read")
	require.True(t, repo.scopedReads.Load() >= 2,
		"Get must always re-read through the scope-bound context")
}
