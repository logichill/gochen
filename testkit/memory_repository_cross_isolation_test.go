package testkit_test

import (
	"context"
	"testing"

	"gochen/contextx"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
)

// L2 跨隔离只读阀门的内存侧行为特征测试。
//
// 与 runtime/db/orm/repo/cross_isolation_test.go 覆盖**同一组语义事实**，
// 两侧共同保证跨存储一致。SQL 侧另有两条内存侧无对应形态的用例
// （资源边界解析、require-scope 边界），不在此重复。

type recordingCrossIsolationAudit struct {
	records  []testkit.CrossIsolationRead
	failWith error
}

func (a *recordingCrossIsolationAudit) RecordCrossIsolationRead(_ context.Context, read testkit.CrossIsolationRead) error {
	if a.failWith != nil {
		return a.failWith
	}
	a.records = append(a.records, read)
	return nil
}

func newCrossIsolationRepo(t *testing.T, audit testkit.ICrossIsolationAudit) *testkit.MemoryRepository[*isoDoc, int64] {
	t.Helper()
	return testkit.NewMemoryRepository[*isoDoc, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryIsolation[*isoDoc, int64](),
		testkit.WithMemoryCrossIsolationReads[*isoDoc, int64](audit),
	)
}

func crossIsolationCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, err := contextx.WithCrossIsolation(context.Background(), contextx.CrossIsolation{
		Reason:   "monthly_billing_reconciliation",
		Operator: "platform_cron_worker",
	})
	require.NoError(t, err)
	return ctx
}

func seedTwoTenants(t *testing.T, repo *testkit.MemoryRepository[*isoDoc, int64]) {
	t.Helper()
	require.NoError(t, repo.Create(tenantCtx(t, "tenant-a"), &isoDoc{Title: "a"}))
	require.NoError(t, repo.Create(tenantCtx(t, "tenant-b"), &isoDoc{Title: "b"}))
}

// 1) 两道闸都合上：读跨越全部隔离空间，且留下一条痕迹。
func TestMemoryCrossIsolation_ReadsSpanAllSpacesAndAreAudited(t *testing.T) {
	audit := &recordingCrossIsolationAudit{}
	repo := newCrossIsolationRepo(t, audit)
	seedTwoTenants(t, repo)

	docs, err := repo.List(crossIsolationCtx(t), 0, 0)
	require.NoError(t, err)
	require.Equal(t, 2, len(docs))

	require.Equal(t, 1, len(audit.records))
	require.Equal(t, "monthly_billing_reconciliation", audit.records[0].Reason)
	require.Equal(t, "platform_cron_worker", audit.records[0].Operator)

	count, err := repo.Count(crossIsolationCtx(t))
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	require.Equal(t, 2, len(audit.records))

	// 单体读同样放行：数据迁移与巡检需要按 ID 取到别的隔离空间的记录。
	got, err := repo.Get(crossIsolationCtx(t), 2)
	require.NoError(t, err)
	require.Equal(t, "b", got.Title)
}

// 2) 缺请求期凭据：声明了阀门的仓储在普通请求上依然严格隔离，且不留痕。
func TestMemoryCrossIsolation_WithoutCredentialStaysIsolated(t *testing.T) {
	audit := &recordingCrossIsolationAudit{}
	repo := newCrossIsolationRepo(t, audit)
	seedTwoTenants(t, repo)

	docs, err := repo.List(tenantCtx(t, "tenant-a"), 0, 0)
	require.NoError(t, err)
	require.Equal(t, 1, len(docs))
	require.Equal(t, "a", docs[0].Title)

	_, err = repo.Get(tenantCtx(t, "tenant-b"), 1)
	require.True(t, errors.Is(err, errors.NotFound))

	_, err = repo.Count(context.Background())
	require.True(t, errors.Is(err, errors.Forbidden))

	require.Equal(t, 0, len(audit.records))
}

// 3) 缺构造期声明：凭据落到未声明阀门的仓储上完全惰性。
func TestMemoryCrossIsolation_CredentialIsInertWithoutDeclaration(t *testing.T) {
	repo := newIsolatedRepo(t)
	seedTwoTenants(t, repo)

	_, err := repo.Count(crossIsolationCtx(t))
	require.True(t, errors.Is(err, errors.Forbidden))

	ctx, err := contextx.WithTenantID(crossIsolationCtx(t), "tenant-a")
	require.NoError(t, err)
	docs, err := repo.List(ctx, 0, 0)
	require.NoError(t, err)
	require.Equal(t, 1, len(docs))
}

// 4) 留痕失败即拒绝。
func TestMemoryCrossIsolation_AuditFailureBlocksRead(t *testing.T) {
	audit := &recordingCrossIsolationAudit{
		failWith: errors.NewCode(errors.Internal, "audit sink unavailable"),
	}
	repo := newCrossIsolationRepo(t, audit)
	seedTwoTenants(t, repo)

	_, err := repo.List(crossIsolationCtx(t), 0, 0)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 5) 写路径永不放行，且数据一条都不动。
func TestMemoryCrossIsolation_WritesAreAlwaysRefused(t *testing.T) {
	audit := &recordingCrossIsolationAudit{}
	repo := newCrossIsolationRepo(t, audit)
	seedTwoTenants(t, repo)

	ctx := crossIsolationCtx(t)
	require.True(t, errors.Is(repo.Create(ctx, &isoDoc{Title: "c"}), errors.Forbidden))
	require.True(t, errors.Is(repo.Update(ctx, &isoDoc{Title: "tampered"}), errors.Forbidden))
	require.True(t, errors.Is(repo.Delete(ctx, 1), errors.Forbidden))
	require.True(t, errors.Is(repo.Purge(ctx, 1), errors.Forbidden))
	require.True(t, errors.Is(repo.CreateAll(ctx, []*isoDoc{{Title: "d"}}), errors.Forbidden))
	require.True(t, errors.Is(repo.UpdateAll(ctx, []*isoDoc{{Title: "e"}}), errors.Forbidden))
	require.True(t, errors.Is(repo.DeleteAll(ctx, []int64{1}), errors.Forbidden))

	// 带着隔离键的写同样被拒。
	writeCtx, err := contextx.WithTenantID(crossIsolationCtx(t), "tenant-a")
	require.NoError(t, err)
	require.True(t, errors.Is(repo.Delete(writeCtx, 1), errors.Forbidden))

	docs, err := repo.List(crossIsolationCtx(t), 0, 0)
	require.NoError(t, err)
	require.Equal(t, 2, len(docs))
	require.Equal(t, "a", docs[0].Title)
}

//  6. 构造期 fail-fast：没有留痕出口、或没有隔离可放的声明一律 panic
//     （测试替身的 fail-fast 等价于生产侧构造期报错）。
func TestMemoryCrossIsolation_DeclarationIsValidatedAtConstruction(t *testing.T) {
	requirePanic(t, func() {
		testkit.NewMemoryRepository[*isoDoc, int64](
			testkit.NewInt64Sequence(1),
			testkit.WithMemoryIsolation[*isoDoc, int64](),
			testkit.WithMemoryCrossIsolationReads[*isoDoc, int64](nil),
		)
	})
	requirePanic(t, func() {
		testkit.NewMemoryRepository[*isoDoc, int64](
			testkit.NewInt64Sequence(1),
			testkit.WithMemoryCrossIsolationReads[*isoDoc, int64](&recordingCrossIsolationAudit{}),
		)
	})
}

func requirePanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
