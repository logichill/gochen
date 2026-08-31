package scoped_test

import (
	"context"
	"strconv"
	"testing"

	secscoped "gochen/app/security/scoped"

	"gochen/app/crud"
	authscoped "gochen/auth/scoped"
	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
)

// L3 批量写的两条不变量：
//   - 授权成本（边界解析 + PDP 判定）不得先于批量上限校验发生；
//   - 边界解析优先走批量探针，缺探针时才逐条降级。

// batchProbeCounter 记录一次批量写实际付出的解析与判定次数。
type batchProbeCounter struct {
	singleCalls    int
	batchCalls     int
	batchIDs       []int64
	authorizeCalls int
}

func (c *batchProbeCounter) reset() { *c = batchProbeCounter{} }

// singleBoundaryRepo 只提供单条边界探针，代表"没有批量解析能力"的仓储。
type singleBoundaryRepo struct {
	*testkit.MemoryRepository[*order, int64]
	counter *batchProbeCounter
}

func (r *singleBoundaryRepo) ResolveResourceByID(_ context.Context, id int64) (authscoped.Resource, error) {
	r.counter.singleCalls++
	return boundaryResourceOf(id), nil
}

// batchBoundaryRepo 在单条探针之外补上批量探针。
type batchBoundaryRepo struct {
	*singleBoundaryRepo
}

func (r *batchBoundaryRepo) ResolveResourcesByID(_ context.Context, ids []int64) ([]authscoped.Resource, error) {
	r.counter.batchCalls++
	r.counter.batchIDs = append(r.counter.batchIDs, ids...)
	resources := make([]authscoped.Resource, 0, len(ids))
	for _, id := range ids {
		resources = append(resources, boundaryResourceOf(id))
	}
	return resources, nil
}

// shortBoundaryRepo 的批量探针少返回一条边界，模拟实现方违约。
type shortBoundaryRepo struct {
	*singleBoundaryRepo
}

func (r *shortBoundaryRepo) ResolveResourcesByID(_ context.Context, ids []int64) ([]authscoped.Resource, error) {
	r.counter.batchCalls++
	return []authscoped.Resource{boundaryResourceOf(ids[0])}, nil
}

func boundaryResourceOf(id int64) authscoped.Resource {
	// Revision 取行内真实版本（本夹具创建的实体版本为 0）——
	// 内存仓储与 SQL 仓储同样要求约束携带乐观锁基线。
	return authscoped.Resource{Kind: "order", ID: strconv.FormatInt(id, 10), ManagedScopeID: 101, Revision: "0"}
}

func newSingleBoundaryRepo(counter *batchProbeCounter) *singleBoundaryRepo {
	return &singleBoundaryRepo{
		MemoryRepository: testkit.NewMemoryRepository[*order, int64](
			testkit.NewInt64Sequence(1),
			testkit.WithMemoryScope[*order, int64]("order"),
		),
		counter: counter,
	}
}

// countingAuthorizer 统计 PDP 判定次数，并按目标自身的 ID 授权。
func countingAuthorizer(t *testing.T, counter *batchProbeCounter) authscoped.IAuthorizer {
	t.Helper()
	evaluator := authscoped.EvaluatorFunc(func(_ context.Context, _ string, resources []authscoped.Resource) (authscoped.Decision, error) {
		counter.authorizeCalls++
		return authscoped.Allow(resources...), nil
	})
	resolver := authscoped.ResourceResolverFunc(func(target any) (authscoped.Resource, bool) {
		o, ok := target.(*order)
		if !ok {
			return authscoped.Resource{}, false
		}
		return boundaryResourceOf(o.ID), true
	})
	authorizer, err := authscoped.NewAuthorizer(resolver, evaluator)
	require.NoError(t, err)
	return authorizer
}

func newBatchApp(t *testing.T, repo domcrud.IRepository[*order, int64], maxBatch int) crud.IApplication[*order, int64] {
	t.Helper()
	cfg := crud.DefaultServiceConfig()
	cfg.MaxBatchSize = maxBatch
	app, err := crud.NewApplication[*order, int64](repo, nil, cfg)
	require.NoError(t, err)
	return app
}

// batchSurface 是 L3 组合外壳对外暴露的批量写能力面。
type batchSurface interface {
	CreateAll(ctx context.Context, entities []*order) error
	UpdateAll(ctx context.Context, entities []*order) error
	DeleteAll(ctx context.Context, ids []int64) error
}

func newSecuredBatch(t *testing.T, repo domcrud.IRepository[*order, int64], counter *batchProbeCounter, maxBatch int) batchSurface {
	t.Helper()
	secured, err := secscoped.New[*order, int64](
		newBatchApp(t, repo, maxBatch), countingAuthorizer(t, counter), config())
	require.NoError(t, err)
	batch, ok := secured.(batchSurface)
	require.True(t, ok, "L3 组合外壳必须保住批量写能力面")
	return batch
}

func ordersWithIDs(ids ...int64) []*order {
	entities := make([]*order, 0, len(ids))
	for _, id := range ids {
		entities = append(entities, &order{
			Entity:  domcrud.Entity[int64]{ID: id},
			ScopeID: 101,
			Name:    "n" + strconv.FormatInt(id, 10),
		})
	}
	return entities
}

// --- 批量上限必须先于授权成本 ---

// 超出批量上限的请求必须在任何边界解析与 PDP 判定之前被拒绝。
//
// 内层 Application 也校验同一个上限，但那发生在 L3 转发之后：等它来拒，
// N 次边界解析、N 次 PDP、N 条授权审计的成本已经全部付掉了。
func TestBatchWriteRejectsOversizedBatchBeforeAnyAuthorization(t *testing.T) {
	counter := &batchProbeCounter{}
	repo := &batchBoundaryRepo{singleBoundaryRepo: newSingleBoundaryRepo(counter)}
	batch := newSecuredBatch(t, repo, counter, 2)
	ctx := context.Background()

	cases := map[string]func() error{
		"CreateAll": func() error { return batch.CreateAll(ctx, ordersWithIDs(1, 2, 3)) },
		"UpdateAll": func() error { return batch.UpdateAll(ctx, ordersWithIDs(1, 2, 3)) },
		"DeleteAll": func() error { return batch.DeleteAll(ctx, []int64{1, 2, 3}) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			counter.reset()

			err := call()
			require.Error(t, err)
			require.True(t, errors.Is(err, errors.Validation))
			require.Equal(t, 0, counter.batchCalls, "超限请求不得触发任何边界解析")
			require.Equal(t, 0, counter.singleCalls, "超限请求不得触发任何边界解析")
			require.Equal(t, 0, counter.authorizeCalls, "超限请求不得触发任何 PDP 判定")
		})
	}
}

// 恰好等于上限的批量正常放行——校验的是 ">"，不是 ">="。
func TestBatchWriteAcceptsBatchAtLimit(t *testing.T) {
	counter := &batchProbeCounter{}
	repo := &batchBoundaryRepo{singleBoundaryRepo: newSingleBoundaryRepo(counter)}
	batch := newSecuredBatch(t, repo, counter, 2)

	require.NoError(t, batch.CreateAll(context.Background(), ordersWithIDs(1, 2)))
}

// --- 边界解析批量化 ---

// 批量写的边界解析必须只查一次，且结果与入参严格同序。
func TestBatchWriteResolvesBoundariesInOneRoundTrip(t *testing.T) {
	counter := &batchProbeCounter{}
	repo := &batchBoundaryRepo{singleBoundaryRepo: newSingleBoundaryRepo(counter)}
	batch := newSecuredBatch(t, repo, counter, 10)
	ctx := context.Background()

	require.NoError(t, batch.CreateAll(ctx, ordersWithIDs(1, 2, 3)))
	counter.reset()

	require.NoError(t, batch.DeleteAll(ctx, []int64{1, 2, 3}))
	require.Equal(t, 1, counter.batchCalls, "批量写只应解析一次边界")
	require.Equal(t, 0, counter.singleCalls, "有批量探针时不得再逐条解析")
	require.Equal(t, []int64{1, 2, 3}, counter.batchIDs)
	require.Equal(t, 3, counter.authorizeCalls, "PDP 仍逐条判定，保证拒绝可归因")
}

// 仓储只有单条探针时降级为逐条解析，语义不变。
func TestBatchWriteFallsBackToPerItemBoundaryProbe(t *testing.T) {
	counter := &batchProbeCounter{}
	repo := newSingleBoundaryRepo(counter)
	batch := newSecuredBatch(t, repo, counter, 10)
	ctx := context.Background()

	require.NoError(t, batch.CreateAll(ctx, ordersWithIDs(1, 2, 3)))
	counter.reset()

	require.NoError(t, batch.DeleteAll(ctx, []int64{1, 2, 3}))
	require.Equal(t, 0, counter.batchCalls)
	require.Equal(t, 3, counter.singleCalls, "无批量探针时逐条降级")
}

// 批量探针少返回一条边界即整批拒绝：少一条边界就是少一次授权判定。
func TestBatchWriteRejectsIncompleteBoundaryResult(t *testing.T) {
	counter := &batchProbeCounter{}
	repo := &shortBoundaryRepo{singleBoundaryRepo: newSingleBoundaryRepo(counter)}
	batch := newSecuredBatch(t, repo, counter, 10)

	err := batch.DeleteAll(context.Background(), []int64{1, 2, 3})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.Equal(t, 0, counter.authorizeCalls, "边界不全时不得进入 PDP 判定")
}
