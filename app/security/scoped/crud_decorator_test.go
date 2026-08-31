package scoped_test

import (
	"context"
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

type order struct {
	domcrud.Entity[int64]
	ScopeID int64
	Name    string
}

func (o *order) Validate() error { return nil }

// scopeAwareRepo 模拟一个"已声明范围列"的仓储：
// 实现 IScopeDeclarationProbe，并在写路径从受控通道读取约束。
type scopeAwareRepo struct {
	*testkit.MemoryRepository[*order, int64]
	declared bool

	// 记录写路径实际读到的约束，用于断言通道确实打通。
	lastCreateConstraint authscoped.WriteConstraint
	createConstraintOK   bool
}

type declarationOnlyRepo struct {
	domcrud.IRepository[*order, int64]
}

func (r *declarationOnlyRepo) HasScopeDeclaration() bool { return true }

func (r *scopeAwareRepo) HasScopeDeclaration() bool { return r.declared }

// ResourceKind 与下面 Create 里读取约束所用的实体类型保持一致——
// 两者不一致时约束恒匹配不到，装配层会在构造期拦下。
func (r *scopeAwareRepo) ResourceKind() string { return "order" }

func (r *scopeAwareRepo) Create(ctx context.Context, e *order) error {
	constraint, ok := authscoped.ConstraintFrom(ctx, "order")
	r.lastCreateConstraint, r.createConstraintOK = constraint, ok
	if !ok {
		// Repo 侧 fail-closed：取不到约束即拒绝，绝不静默放行。
		return errors.NewCode(errors.Forbidden, "write constraint is required but missing")
	}
	return r.MemoryRepository.Create(ctx, e)
}

func newRepo(declared bool) *scopeAwareRepo {
	return &scopeAwareRepo{
		MemoryRepository: testkit.NewMemoryRepository[*order, int64](testkit.NewInt64Sequence(1)),
		declared:         declared,
	}
}

func newApp(t *testing.T, repo domcrud.IRepository[*order, int64]) crud.IApplication[*order, int64] {
	t.Helper()
	app, err := crud.NewApplication[*order, int64](repo, nil, nil)
	require.NoError(t, err)
	return app
}

func fullPolicy() action.OperationPolicy {
	return action.OperationPolicy{
		Create: "order:api:create",
		Read:   "order:api:read",
		Update: "order:api:update",
		Delete: "order:api:delete",
		List:   "order:api:list",
	}
}

func allowAuthorizer(t *testing.T, scopeID int64) authscoped.IAuthorizer {
	t.Helper()
	evaluator := authscoped.EvaluatorFunc(func(_ context.Context, _ string, resources []authscoped.Resource) (authscoped.Decision, error) {
		if len(resources) == 0 {
			return authscoped.Allow(authscoped.Resource{Kind: "order", ID: "*", ManagedScopeID: scopeID}), nil
		}
		return authscoped.Allow(resources...), nil
	})
	resolver := authscoped.ResourceResolverFunc(func(target any) (authscoped.Resource, bool) {
		o, ok := target.(*order)
		if !ok {
			return authscoped.Resource{}, false
		}
		return authscoped.Resource{Kind: "order", ID: "1", ManagedScopeID: o.ScopeID}, true
	})
	authorizer, err := authscoped.NewAuthorizer(resolver, evaluator)
	require.NoError(t, err)
	return authorizer
}

func denyAuthorizer(t *testing.T) authscoped.IAuthorizer {
	t.Helper()
	evaluator := authscoped.EvaluatorFunc(func(context.Context, string, []authscoped.Resource) (authscoped.Decision, error) {
		return authscoped.Deny(authscoped.ReasonOutOfScope), nil
	})
	authorizer, err := authscoped.NewAuthorizer(
		authscoped.ResourceResolverFunc(func(any) (authscoped.Resource, bool) {
			return authscoped.Resource{Kind: "order", ID: "1"}, true
		}), evaluator)
	require.NoError(t, err)
	return authorizer
}

func config() secscoped.Config {
	return secscoped.Config{
		EntityType:    "order",
		Policy:        fullPolicy(),
		ScopeResolver: authscoped.DataScopeResolverFunc(func(context.Context) (authscoped.DataScope, error) { return authscoped.Filtered(101), nil }),
	}
}

// --- 构造期断言：防静默降级 ---

// 未声明范围列的仓储参与 L3 装配必须启动期被拒绝。
// 这是最危险的失败模式：约束被静默丢弃 → 请求全部放行。
func TestNewRejectsRepositoryWithoutScopeDeclaration(t *testing.T) {
	app := newApp(t, newRepo(false))
	_, err := secscoped.New[*order, int64](app, allowAuthorizer(t, 101), config())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// 未声明范围能力的内存仓储同样被拒绝。
//
// 内存仓储实现了探针接口但声明为 false，因此报 InvalidInput
// （"没声明范围列"），而不是 Unsupported（"根本没有探针"）。
func TestNewRejectsMemoryRepositoryWithoutScopeDeclaration(t *testing.T) {
	plain := testkit.NewMemoryRepository[*order, int64](testkit.NewInt64Sequence(1))
	app := newApp(t, plain)
	_, err := secscoped.New[*order, int64](app, allowAuthorizer(t, 101), config())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestNewRejectsRepositoryWithoutRequiredScopeProbes(t *testing.T) {
	base := testkit.NewMemoryRepository[*order, int64](testkit.NewInt64Sequence(1))
	app := newApp(t, &declarationOnlyRepo{IRepository: base})
	_, err := secscoped.New[*order, int64](app, allowAuthorizer(t, 101), config())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Unsupported))
}

func TestNewValidatesWiring(t *testing.T) {
	app := newApp(t, newRepo(true))

	_, err := secscoped.New[*order, int64](nil, allowAuthorizer(t, 101), config())
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = secscoped.New[*order, int64](app, nil, config())
	require.True(t, errors.Is(err, errors.InvalidInput))

	// EntityType 为空 → 约束无法定向投放
	cfg := config()
	cfg.EntityType = ""
	_, err = secscoped.New[*order, int64](app, allowAuthorizer(t, 101), cfg)
	require.True(t, errors.Is(err, errors.InvalidInput))

	// 非法权限码在装配期暴露
	cfg = config()
	cfg.Policy = action.OperationPolicy{Create: "api:order"}
	_, err = secscoped.New[*order, int64](app, allowAuthorizer(t, 101), cfg)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// --- 受控约束通道打通 ---

// PDP 判定通过后，约束必须经通道到达 Repo 写路径。
func TestConstraintReachesRepositoryOnWrite(t *testing.T) {
	repo := newRepo(true)
	secured, err := secscoped.New[*order, int64](newApp(t, repo), allowAuthorizer(t, 101), config())
	require.NoError(t, err)

	require.NoError(t, secured.Create(context.Background(), &order{ScopeID: 101, Name: "n1"}))

	require.True(t, repo.createConstraintOK, "Repo 必须能从受控通道读到约束")
	resource, ok := repo.lastCreateConstraint.Single()
	require.True(t, ok)
	require.Equal(t, "order", resource.Kind)
	require.Equal(t, int64(101), resource.ManagedScopeID)
}

// 绕过 L3 装饰器直接调用基础 Application，Repo 读不到约束 → fail-closed。
// 这正是"全入口一致"的体现：安全不依赖 Transport。
func TestBypassingDecoratorFailsClosed(t *testing.T) {
	repo := newRepo(true)
	plain := newApp(t, repo)

	err := plain.Create(context.Background(), &order{ScopeID: 101, Name: "n1"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.False(t, repo.createConstraintOK)
}

// PDP 拒绝时不得触达仓储。
func TestDeniedWriteNeverReachesRepository(t *testing.T) {
	repo := newRepo(true)
	secured, err := secscoped.New[*order, int64](newApp(t, repo), denyAuthorizer(t), config())
	require.NoError(t, err)

	err = secured.Create(context.Background(), &order{ScopeID: 101})
	require.True(t, errors.Is(err, errors.Forbidden))

	count, err := repo.MemoryRepository.Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
}

// --- 读路径与 DataScope 三态 ---

// ScopeDenyAll 时读路径强制拒绝（§4.1 铁律）。
func TestDenyAllScopeBlocksReads(t *testing.T) {
	cfg := config()
	cfg.ScopeResolver = authscoped.DataScopeResolverFunc(func(context.Context) (authscoped.DataScope, error) {
		return authscoped.DenyAll(), nil
	})
	secured, err := secscoped.New[*order, int64](newApp(t, newRepo(true)), allowAuthorizer(t, 101), cfg)
	require.NoError(t, err)

	_, err = secured.List(context.Background(), 0, 10)
	require.True(t, errors.Is(err, errors.Forbidden))

	_, err = secured.Count(context.Background())
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 读路径把解析出的范围绑定到 ctx，供下层仓储消费。
func TestReadBindsDataScopeToContext(t *testing.T) {
	repo := newRepo(true)
	secured, err := secscoped.New[*order, int64](newApp(t, repo), allowAuthorizer(t, 101), config())
	require.NoError(t, err)

	_, err = secured.List(context.Background(), 0, 10)
	require.NoError(t, err)
}

// 未配置的操作 fail-closed。
func TestUnconfiguredOperationFailsClosed(t *testing.T) {
	cfg := config()
	cfg.Policy = action.OperationPolicy{Create: "order:api:create"}
	secured, err := secscoped.New[*order, int64](newApp(t, newRepo(true)), allowAuthorizer(t, 101), cfg)
	require.NoError(t, err)

	_, err = secured.List(context.Background(), 0, 10)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// Exists 与 L1 一致使用 read 动作码，只配置 Read 时即可调用，无需 List 权限。
func TestExistsMapsToReadAction(t *testing.T) {
	repo := newRepo(true)
	cfg := config()
	cfg.Policy = action.OperationPolicy{
		Create: "order:api:create",
		Read:   "order:api:read",
	}
	secured, err := secscoped.New[*order, int64](newApp(t, repo), allowAuthorizer(t, 101), cfg)
	require.NoError(t, err)

	_, err = secured.Exists(context.Background(), 1)
	require.NoError(t, err)
}

// --- 可选能力保持 ---

// R5 关键回归：L3 装饰同样不得丢失底层可选能力接口。
func TestDecoratorPreservesOptionalCapabilities(t *testing.T) {
	secured, err := secscoped.New[*order, int64](newApp(t, newRepo(true)), allowAuthorizer(t, 101), config())
	require.NoError(t, err)

	_, ok := secured.Repository().(domcrud.IQueryRepository[*order, int64])
	require.True(t, ok)

	queryRepo, ok := secured.QueryRepository()
	require.True(t, ok)
	require.NotNil(t, queryRepo)
	require.NotNil(t, secured.Config())
}
