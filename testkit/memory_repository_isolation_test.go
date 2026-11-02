package testkit_test

import (
	"context"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
	"testing"
)

// L2 空间隔离行为特征测试。
//
// 本套用例定义"隔离"在语义上必须成立的事实。SQL 实现
// （runtime/db/orm/repo）需通过等价的一组断言，
// 两侧共同保证跨存储语义一致——R5 取消 Core Enforcer 接口后，
// 一致性由测试而非接口强制。

type isoDoc struct {
	auth.TenantEntity[int64]
	Title string `json:"title"`
}

func (d *isoDoc) Validate() error { return nil }

func tenantCtx(t *testing.T, tenantID string) context.Context {
	t.Helper()
	ctx, err := contextx.WithTenantID(context.Background(), tenantID)
	require.NoError(t, err)
	return ctx
}

func newIsolatedRepo(t *testing.T) *testkit.MemoryRepository[*isoDoc, int64] {
	t.Helper()
	repository, repositoryErr := testkit.NewMemoryRepository[*isoDoc, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryIsolation[*isoDoc, int64](),
	)
	if repositoryErr !=

		// 1) 写路径：Create 必须标记归属。
		nil {
		t.Fatal(repositoryErr)
	}
	return repository
}

func TestIsolation_CreateStampsOwnership(t *testing.T) {
	repo := newIsolatedRepo(t)
	doc := &isoDoc{Title: "a"}
	require.NoError(t, repo.Create(tenantCtx(t, "tenant-a"), doc, spec.All()))
	require.Equal(t, "tenant-a", doc.GetTenantID())
}

// 2) 读路径：返回数据必须只属于当前隔离空间。
func TestIsolation_ReadsAreScopedToTenant(t *testing.T) {
	repo := newIsolatedRepo(t)
	ctxA, ctxB := tenantCtx(t, "tenant-a"), tenantCtx(t, "tenant-b")

	docA := &isoDoc{Title: "a"}
	require.NoError(t, repo.Create(ctxA, docA, spec.All()))
	require.NoError(t, repo.Create(ctxB, &isoDoc{Title: "b"}, spec.All()))

	// Get：跨租户读按 NotFound 处理（防探测，不泄露"存在但无权"）
	_, err := repo.Get(ctxB, docA.GetID(), spec.All())
	require.True(t, errors.Is(err, errors.NotFound))

	got, err := repo.Get(ctxA, docA.GetID(), spec.All())
	require.NoError(t, err)
	require.Equal(t, "a", got.Title)

	// List / Count / Exists 一致
	listA, err := repo.List(ctxA, 0, 10, spec.All())
	require.NoError(t, err)
	require.Equal(t, 1, len(listA))
	require.Equal(t, "a", listA[0].Title)

	countB, err := repo.Count(ctxB, spec.All())
	require.NoError(t, err)
	require.Equal(t, int64(1), countB)

	exists, err := repo.Exists(ctxB, docA.GetID(), spec.All())
	require.NoError(t, err)
	require.False(t, exists)
}

// 3) 写守卫：跨租户更新/删除必须失败，且不得改动数据。
func TestIsolation_CrossTenantWritesAreGuarded(t *testing.T) {
	repo := newIsolatedRepo(t)
	ctxA, ctxB := tenantCtx(t, "tenant-a"), tenantCtx(t, "tenant-b")

	docA := &isoDoc{Title: "a"}
	require.NoError(t, repo.Create(ctxA, docA, spec.All()))

	hijack := &isoDoc{Title: "hijacked"}
	hijack.ID = docA.GetID()
	require.True(t, errors.Is(repo.Update(ctxB, hijack, spec.All()), errors.NotFound))
	require.True(t, errors.Is(repo.Delete(ctxB, docA, spec.All()), errors.NotFound))

	// 数据未被篡改
	got, err := repo.Get(ctxA, docA.GetID(), spec.All())
	require.NoError(t, err)
	require.Equal(t, "a", got.Title)
}

// 4) 归属不可被更新篡改：即便请求体带了别的租户，也会被强制改回当前租户。
func TestIsolation_UpdateCannotRewriteOwnership(t *testing.T) {
	repo := newIsolatedRepo(t)
	ctxA := tenantCtx(t, "tenant-a")

	doc := &isoDoc{Title: "a"}
	require.NoError(t, repo.Create(ctxA, doc, spec.All()))

	doc.Title = "a2"
	doc.SetTenantID("tenant-b") // 试图把归属改走
	require.ErrorIs(t, repo.Update(ctxA, doc, spec.All()), errors.Forbidden)
	require.Equal(t, "tenant-b", doc.GetTenantID())

	// 仍留在 tenant-a 可见范围内
	got, err := repo.Get(ctxA, doc.GetID(), spec.All())
	require.NoError(t, err)
	require.Equal(t, "a", got.Title)
}

// 5) Fail-Closed：启用隔离但 ctx 无租户 → 一律拒绝，绝不退化为"看全部"。
func TestIsolation_FailsClosedWithoutTenant(t *testing.T) {
	repo := newIsolatedRepo(t)
	ctx := context.Background()

	require.True(t, errors.Is(repo.Create(ctx, &isoDoc{Title: "x"}, spec.All()), errors.Forbidden))

	_, err := repo.Get(ctx, 1, spec.All())
	require.True(t, errors.Is(err, errors.Forbidden))

	_, err = repo.List(ctx, 0, 10, spec.All())
	require.True(t, errors.Is(err, errors.Forbidden))

	_, err = repo.Count(ctx, spec.All())
	require.True(t, errors.Is(err, errors.Forbidden))

	require.True(t, errors.Is(repo.Delete(ctx, isoDocWithID(1), spec.All()), errors.Forbidden))
}

// 6) 批量路径与单条路径语义一致。
func TestIsolation_BulkPathsAreGuarded(t *testing.T) {
	repo := newIsolatedRepo(t)
	ctxA, ctxB := tenantCtx(t, "tenant-a"), tenantCtx(t, "tenant-b")

	docs := []*isoDoc{{Title: "a1"}, {Title: "a2"}}
	require.NoError(t, repo.CreateBulk(ctxA, docs, spec.All()))
	for _, d := range docs {
		require.Equal(t, "tenant-a", d.GetTenantID())
	}

	// 跨租户批量删除整体失败
	require.True(t, errors.Is(repo.DeleteBulk(ctxB, docs, spec.All()), errors.NotFound))

	countA, err := repo.Count(ctxA, spec.All())
	require.NoError(t, err)
	require.Equal(t, int64(2), countA, "失败的批量删除不得影响数据")

	require.NoError(t, repo.DeleteBulk(ctxA, docs, spec.All()))
	countA, err = repo.Count(ctxA, spec.All())
	require.NoError(t, err)
	require.Equal(t, int64(0), countA)
}

// 6.1) 批量创建失败路径与单条/基线一致：prepare 阶段已盖归属戳，仓储数据原子回退。
func TestIsolation_CreateAllFailureKeepsPrepareStamp(t *testing.T) {
	repo := newIsolatedRepo(t)
	ctxA := tenantCtx(t, "tenant-a")

	doc := &isoDoc{Title: "dup"}
	err := repo.CreateBulk(ctxA, []*isoDoc{doc, doc}, spec.All())
	require.True(t, errors.Is(err, errors.Conflict))
	require.Equal(t, "tenant-a", doc.GetTenantID())

	count, countErr := repo.Count(ctxA, spec.All())
	require.NoError(t, countErr)
	require.Equal(t, int64(0), count, "失败的批量创建不得写入任何数据")
}

// 7) 未启用隔离时行为不变（L0/L1 不背负 L2 心智）。
func TestIsolation_DisabledKeepsPlainBehaviour(t *testing.T) {
	repo, repositoryErr := testkit.NewMemoryRepository[*isoDoc, int64](testkit.NewInt64Sequence(1))
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}
	ctx := context.Background()

	doc := &isoDoc{Title: "a"}
	require.NoError(t, repo.Create(ctx, doc, spec.All()))
	require.Equal(t, "", doc.GetTenantID(), "未启用隔离时不得擅自标记归属")

	got, err := repo.Get(ctx, doc.GetID(), spec.All())
	require.NoError(t, err)
	require.Equal(t, "a", got.Title)
}

// 8) 实体无法承载归属时，启用隔离必须 fail-fast（与 SQL 侧构造期断言等价）。
type isoPlainDoc struct {
	crud.Entity[int64]
	Title string
}

func TestIsolation_RequiresTenantAwareEntity(t *testing.T) {
	_, err := testkit.NewMemoryRepository[*isoPlainDoc, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryIsolation[*isoPlainDoc, int64](),
	)
	require.ErrorIs(t, err, errors.InvalidInput)
}

// 9) 隔离键来源可换：与 SQL 侧 IsolationCols.Resolve 同构。
//
// 内存仓储没有"列"的概念，归属槽位固定是 ITenantEntity，可换的只有取值来源。
// 这三个用例与 runtime 的 isolation_test.go 中对应用例断言同一组事实。

type memOrgKey struct{}

func memOrgCtx(t *testing.T, tenantID, org string) context.Context {
	t.Helper()
	ctx := tenantCtx(t, tenantID)
	return context.WithValue(ctx, memOrgKey{}, org)
}

func memOrgResolver(ctx context.Context) (string, error) {
	org, _ := ctx.Value(memOrgKey{}).(string)
	return org, nil
}

func newOrgIsolatedRepo(t *testing.T) *testkit.MemoryRepository[*isoDoc, int64] {
	t.Helper()
	repository, repositoryErr := testkit.NewMemoryRepository[*isoDoc, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryIsolationResolver[*isoDoc, int64](memOrgResolver),
	)
	if repositoryErr !=

		// 归属来自自定义来源，而不是 contextx.TenantID——ctx 里那个不同的租户不得污染归属。
		nil {
		t.Fatal(repositoryErr)
	}
	return repository
}

func TestIsolation_CustomResolveDrivesOwnership(t *testing.T) {
	repo := newOrgIsolatedRepo(t)
	ctx := memOrgCtx(t, "tenant-a", "org-7")

	doc := &isoDoc{Title: "x"}
	require.NoError(t, repo.Create(ctx, doc, spec.All()))
	require.Equal(t, "org-7", doc.GetTenantID())

	// 同租户但不同 org → 看不见。
	_, err := repo.Get(memOrgCtx(t, "tenant-a", "org-9"), doc.GetID(), spec.All())
	require.True(t, errors.Is(err, errors.NotFound))

	// 同 org → 看得见。
	got, err := repo.Get(memOrgCtx(t, "tenant-a", "org-7"), doc.GetID(), spec.All())
	require.NoError(t, err)
	require.Equal(t, "x", got.Title)
}

// 自定义来源解析不到 → fail-closed，绝不回退到租户槽位。
func TestIsolation_CustomResolveMissingKeyFailsClosed(t *testing.T) {
	repo := newOrgIsolatedRepo(t)

	// 租户在，org 不在。
	_, err := repo.Get(tenantCtx(t, "tenant-a"), 1, spec.All())
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 解析器自身报错必须原样上抛，不得被当成"无隔离"而放行。
func TestIsolation_ResolverErrorIsNotSwallowed(t *testing.T) {
	sentinel := errors.NewCode(errors.ServiceUnavailable, "shard directory is down")
	repo, repositoryErr := testkit.NewMemoryRepository[*isoDoc, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryIsolationResolver[*isoDoc, int64](
			func(context.Context) (string, error) { return "", sentinel },
		),
	)
	if repositoryErr != nil {
		t.Fatal(repositoryErr)
	}

	err := repo.Create(context.Background(), &isoDoc{Title: "x"}, spec.All())
	require.True(t, errors.Is(err, errors.ServiceUnavailable))
}
