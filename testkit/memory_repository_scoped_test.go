package testkit_test

import (
	"context"
	"testing"

	"gochen/testkit/require"

	"gochen/auth/scoped"
	"gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
)

// 受控约束通道的跨存储一致性用例。
//
// 与 runtime/db/orm/repo/constraint_channel_test.go 一一对应：
// 内存实现与 SQL 实现在"未命中约束"这件事上必须给出同样的结论。

type scopedMemoryEntity struct {
	crud.Entity[int64]
	Name string `json:"name"`
}

func (e *scopedMemoryEntity) Validate() error { return nil }

func newScopedMemoryRepo(t *testing.T) *testkit.MemoryRepository[*scopedMemoryEntity, int64] {
	t.Helper()
	return testkit.NewMemoryRepository[*scopedMemoryEntity, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryScope[*scopedMemoryEntity, int64]("order"),
	)
}

func orderConstraintContext(entityType, resourceID string) context.Context {
	return scoped.WithConstraint(context.Background(),
		scoped.SingleEntityConstraint(entityType, scoped.WriteConstraint{
			Resources: []scoped.ResourceConstraint{{Kind: entityType, ResourceID: resourceID}},
		}))
}

func revisionConstraintContext(entityType, resourceID, revision string) context.Context {
	return scoped.WithConstraint(context.Background(),
		scoped.SingleEntityConstraint(entityType, scoped.WriteConstraint{
			Resources: []scoped.ResourceConstraint{{Kind: entityType, ResourceID: resourceID, Revision: revision}},
		}))
}

func TestMemoryRepository_ScopeDeclared_RejectsWriteWithoutConstraint(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1

	err := repo.Create(context.Background(), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))

	err = repo.Update(context.Background(), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))

	err = repo.Delete(context.Background(), 1)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

func TestMemoryRepository_ScopeDeclared_RejectsBatchWriteWithoutConstraint(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	first := &scopedMemoryEntity{Name: "n1"}
	first.ID = 1
	second := &scopedMemoryEntity{Name: "n2"}
	second.ID = 2

	err := repo.CreateAll(context.Background(), []*scopedMemoryEntity{first, second})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))

	ctx := scoped.WithConstraint(context.Background(),
		scoped.SingleEntityConstraint("order", scoped.WriteConstraint{Resources: []scoped.ResourceConstraint{
			{Kind: "order", ResourceID: "1"},
			{Kind: "order", ResourceID: "2"},
		}}))
	require.NoError(t, repo.CreateAll(ctx, []*scopedMemoryEntity{first, second}))

	err = repo.UpdateAll(context.Background(), []*scopedMemoryEntity{first, second})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))

	err = repo.DeleteAll(context.Background(), []int64{1, 2})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

func TestMemoryRepository_ScopeDeclared_RejectsForeignEntityType(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1

	err := repo.Create(orderConstraintContext("audit_log", "1"), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

func TestMemoryRepository_ScopeDeclared_AllowsMatchingConstraint(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1

	require.NoError(t, repo.Create(orderConstraintContext("order", "1"), entity))

	got, err := repo.Get(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "n1", got.Name)
}

// 约束授权的是另一条资源时同样拒绝——授权不能跨行复用。
func TestMemoryRepository_ScopeDeclared_RejectsForeignResourceID(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1

	err := repo.Create(orderConstraintContext("order", "999"), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 未声明范围能力的内存仓储不受影响（L0/L1/L2 原样可用）。
func TestMemoryRepository_WithoutScopeDeclaration_IsUnaffected(t *testing.T) {
	repo := testkit.NewMemoryRepository[*scopedMemoryEntity, int64](testkit.NewInt64Sequence(1))
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1

	require.NoError(t, repo.Create(context.Background(), entity))
	require.False(t, repo.HasScopeDeclaration())
}

// --- 约束写的乐观锁语义（与 SQL 侧 writeconstraint.go 对齐） ---

// update / delete 的约束必须携带 revision；缺失是装配错误，不是权限结论。
func TestMemoryRepository_ConstrainedUpdateRequiresRevision(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1
	require.NoError(t, repo.Create(orderConstraintContext("order", "1"), entity))

	err := repo.Update(orderConstraintContext("order", "1"), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = repo.Delete(orderConstraintContext("order", "1"), 1)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// 非法的 revision 同样在写入前拒绝。
func TestMemoryRepository_ConstrainedUpdateRejectsMalformedRevision(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1
	require.NoError(t, repo.Create(orderConstraintContext("order", "1"), entity))

	err := repo.Update(revisionConstraintContext("order", "1", "abc"), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

// 版本基线与实体版本一致时放行。
func TestMemoryRepository_ConstrainedUpdateAcceptsMatchingRevision(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1
	entity.Version = 3
	require.NoError(t, repo.Create(orderConstraintContext("order", "1"), entity))

	updated := &scopedMemoryEntity{Name: "n2"}
	updated.ID = 1
	updated.Version = 3
	require.NoError(t, repo.Update(revisionConstraintContext("order", "1", "3"), updated))

	got, err := repo.Get(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "n2", got.Name)
}

// 调用方实体版本与基线不符 → Concurrency：读到的是过期快照，应重试而非放弃。
func TestMemoryRepository_ConstrainedUpdateRejectsStaleEntityVersion(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1
	entity.Version = 3
	require.NoError(t, repo.Create(orderConstraintContext("order", "1"), entity))

	stale := &scopedMemoryEntity{Name: "n2"}
	stale.ID = 1
	stale.Version = 1

	err := repo.Update(revisionConstraintContext("order", "1", "3"), stale)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Concurrency))

	// 实体版本省略（0）时不做调用方比对——行版本比对兜底。
	fresh := &scopedMemoryEntity{Name: "n3"}
	fresh.ID = 1
	require.NoError(t, repo.Update(revisionConstraintContext("order", "1", "3"), fresh))
}

// 授权与写入之间行版本被推进 → Conflict（等价 SQL 侧 WHERE version 未命中）。
func TestMemoryRepository_ConstrainedUpdateRejectsAdvancedRowVersion(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1
	entity.Version = 3
	require.NoError(t, repo.Create(orderConstraintContext("order", "1"), entity))

	// 授权时读到的是 v1，实际行已是 v3。
	updated := &scopedMemoryEntity{Name: "n2"}
	updated.ID = 1
	err := repo.Update(revisionConstraintContext("order", "1", "1"), updated)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))
}

// delete 同样以 revision 为乐观锁基线：匹配放行，不匹配报 Conflict。
func TestMemoryRepository_ConstrainedDeleteChecksRevision(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1
	entity.Version = 5
	require.NoError(t, repo.Create(orderConstraintContext("order", "1"), entity))

	err := repo.Delete(revisionConstraintContext("order", "1", "4"), 1)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Conflict))

	require.NoError(t, repo.Delete(revisionConstraintContext("order", "1", "5"), 1))
	_, err = repo.Get(context.Background(), 1)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.NotFound))
}

// --- 约束租户盖戳（对齐 SQL 侧 applyConstraintToEntity） ---

type tenantScopedEntity struct {
	crud.TenantEntity[int64]
	Name string `json:"name"`
}

func (e *tenantScopedEntity) Validate() error { return nil }

func newTenantScopedRepo(t *testing.T) *testkit.MemoryRepository[*tenantScopedEntity, int64] {
	t.Helper()
	return testkit.NewMemoryRepository[*tenantScopedEntity, int64](
		testkit.NewInt64Sequence(1),
		testkit.WithMemoryScope[*tenantScopedEntity, int64]("invoice"),
	)
}

// tenantConstraintContext 携带 Revision "0"：夹具实体版本为 0，
// update 路径要求约束带乐观锁基线。
func tenantConstraintContext(resourceID, tenantID string) context.Context {
	return scoped.WithConstraint(context.Background(),
		scoped.SingleEntityConstraint("invoice", scoped.WriteConstraint{
			Resources: []scoped.ResourceConstraint{{
				Kind: "invoice", ResourceID: resourceID, TenantID: tenantID, Revision: "0",
			}},
		}))
}

// create 把约束授权的租户盖到新行上。
func TestMemoryRepository_ConstrainedCreateStampsTenant(t *testing.T) {
	repo := newTenantScopedRepo(t)
	entity := &tenantScopedEntity{Name: "i1"}
	entity.ID = 1

	require.NoError(t, repo.Create(tenantConstraintContext("1", "t1"), entity))

	got, err := repo.Get(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "t1", got.GetTenantID())
}

// 实体已带冲突租户 → 拒绝：归属不可被写入篡改。
func TestMemoryRepository_ConstrainedCreateRejectsTenantMismatch(t *testing.T) {
	repo := newTenantScopedRepo(t)
	entity := &tenantScopedEntity{Name: "i1"}
	entity.ID = 1
	entity.TenantID = "t2"

	err := repo.Create(tenantConstraintContext("1", "t1"), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 实体自带租户与约束一致时原样通过。
func TestMemoryRepository_ConstrainedCreateAcceptsMatchingTenant(t *testing.T) {
	repo := newTenantScopedRepo(t)
	entity := &tenantScopedEntity{Name: "i1"}
	entity.ID = 1
	entity.TenantID = "t1"

	require.NoError(t, repo.Create(tenantConstraintContext("1", "t1"), entity))
}

// update 同样盖租户戳；实体携带冲突租户时拒绝。
func TestMemoryRepository_ConstrainedUpdateStampsTenant(t *testing.T) {
	repo := newTenantScopedRepo(t)
	entity := &tenantScopedEntity{Name: "i1"}
	entity.ID = 1
	require.NoError(t, repo.Create(tenantConstraintContext("1", "t1"), entity))

	updated := &tenantScopedEntity{Name: "i2"}
	updated.ID = 1
	require.NoError(t, repo.Update(tenantConstraintContext("1", "t1"), updated))

	got, err := repo.Get(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "t1", got.GetTenantID())
	require.Equal(t, "i2", got.Name)

	hijacked := &tenantScopedEntity{Name: "i3"}
	hijacked.ID = 1
	hijacked.TenantID = "t2"
	err = repo.Update(tenantConstraintContext("1", "t1"), hijacked)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}

// 约束租户盖不进没有租户槽位的实体 → fail-closed。
func TestMemoryRepository_ConstrainedWriteRejectsTenantWithoutSlot(t *testing.T) {
	repo := newScopedMemoryRepo(t)
	entity := &scopedMemoryEntity{Name: "n1"}
	entity.ID = 1

	err := repo.Create(tenantConstraintContext("1", "t1"), entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}
