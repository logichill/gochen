package scoped_test

import (
	"context"
	"testing"

	secscoped "gochen/app/security/scoped"

	appcrud "gochen/app/crud"
	"gochen/auth/action"
	authscoped "gochen/auth/scoped"
	"gochen/domain/audited"
	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
)

// 审计高危操作的边界解析回归。
//
// Restore 的目标**必然**已软删，Purge / AuditTrail 的目标通常也已软删。
// 若用排除软删的常规探针解析授权输入，这三个操作在 L3 下会恒返回 NotFound——
// 授权还没开始就先 404。旧的 REST 编排走的是 include-deleted 变体，
// 下沉到装饰器时这一点曾经丢失，这里把它锁住。

type doc struct {
	domcrud.Entity[int64]
	Name string
}

func (d *doc) Validate() error { return nil }

// deletedAwareRepo 精确模拟软删记录：常规探针查不到，include-deleted 探针查得到。
type deletedAwareRepo struct {
	*testkit.MemoryRepository[*doc, int64]

	// deletedIDs 中的记录只能被 include-deleted 探针解析到。
	deletedIDs map[int64]bool
	includeHit int
}

func newDeletedAwareRepo(deleted ...int64) *deletedAwareRepo {
	ids := make(map[int64]bool, len(deleted))
	for _, id := range deleted {
		ids[id] = true
	}
	return &deletedAwareRepo{
		MemoryRepository: testkit.NewMemoryRepository[*doc, int64](
			testkit.NewInt64Sequence(1),
			testkit.WithMemoryScope[*doc, int64]("document"),
		),
		deletedIDs: ids,
	}
}

func (r *deletedAwareRepo) ResolveResourceByID(_ context.Context, id int64) (authscoped.Resource, error) {
	if r.deletedIDs[id] {
		return authscoped.Resource{}, errors.NewCode(errors.NotFound, "record not found")
	}
	return r.boundary(id), nil
}

func (r *deletedAwareRepo) ResolveResourceByIDIncludingDeleted(_ context.Context, id int64) (authscoped.Resource, error) {
	r.includeHit++
	return r.boundary(id), nil
}

func (r *deletedAwareRepo) boundary(id int64) authscoped.Resource {
	return authscoped.Resource{Kind: "document", ID: "1", ManagedScopeID: 101, Revision: "0"}
}

// auditedInner 给普通 CRUD Application 补上审计能力面，并记录收到的调用。
type auditedInner struct {
	appcrud.IApplication[*doc, int64]

	purged   []int64
	restored []int64
	trailed  []int64
}

func (a *auditedInner) Purge(_ context.Context, id int64) error {
	a.purged = append(a.purged, id)
	return nil
}

func (a *auditedInner) Restore(_ context.Context, id int64, _ string) error {
	a.restored = append(a.restored, id)
	return nil
}

func (a *auditedInner) ListDeleted(context.Context, int, int) ([]*doc, error) { return nil, nil }

func (a *auditedInner) AuditTrail(_ context.Context, id int64, _, _ int) ([]audited.AuditRecord, error) {
	a.trailed = append(a.trailed, id)
	return nil, nil
}

func (a *auditedInner) AuditStore() audited.IAuditStore { return nil }

func newAuditedL3App(t *testing.T, repo *deletedAwareRepo) (appcrud.IApplication[*doc, int64], *auditedInner) {
	t.Helper()
	plain, err := appcrud.NewApplication[*doc, int64](repo, nil, nil)
	require.NoError(t, err)
	inner := &auditedInner{IApplication: plain}

	evaluator := authscoped.EvaluatorFunc(
		func(_ context.Context, _ string, resources []authscoped.Resource) (authscoped.Decision, error) {
			if len(resources) == 0 {
				return authscoped.Decision{Effect: authscoped.EffectAllow}, nil
			}
			return authscoped.Allow(resources...), nil
		})
	authorizer, err := authscoped.NewAuthorizer(nil, evaluator)
	require.NoError(t, err)

	secured, err := secscoped.New[*doc, int64](inner, authorizer, secscoped.Config{
		EntityType: "document",
		Policy: action.OperationPolicy{
			Create:    "document:api:create",
			Read:      "document:api:read",
			Update:    "document:api:update",
			Delete:    "document:api:delete",
			List:      "document:api:list",
			Restore:   "document:api:restore",
			Purge:     "document:api:purge",
			AuditRead: "document:api:audit_read",
		},
		ScopeResolver: authscoped.DataScopeResolverFunc(
			func(context.Context) (authscoped.DataScope, error) {
				return authscoped.Filtered(101), nil
			}),
	})
	require.NoError(t, err)
	return secured, inner
}

func auditedSurfaceOf(t *testing.T, app appcrud.IApplication[*doc, int64]) interface {
	Purge(ctx context.Context, id int64) error
	Restore(ctx context.Context, id int64, by string) error
	AuditTrail(ctx context.Context, id int64, offset, limit int) ([]audited.AuditRecord, error)
} {
	t.Helper()
	surface, ok := any(app).(interface {
		Purge(ctx context.Context, id int64) error
		Restore(ctx context.Context, id int64, by string) error
		AuditTrail(ctx context.Context, id int64, offset, limit int) ([]audited.AuditRecord, error)
	})
	require.True(t, ok)
	return surface
}

// Restore 的目标已软删：必须经 include-deleted 探针解析边界，而不是先 404。
func TestRestoreResolvesDeletedBoundary(t *testing.T) {
	repo := newDeletedAwareRepo(7)
	secured, inner := newAuditedL3App(t, repo)

	err := auditedSurfaceOf(t, secured).Restore(context.Background(), 7, "operator")

	require.NoError(t, err)
	require.Equal(t, []int64{7}, inner.restored)
	require.Equal(t, 1, repo.includeHit)
}

// Purge 的目标通常已软删，同样走 include-deleted 解析。
func TestPurgeResolvesDeletedBoundary(t *testing.T) {
	repo := newDeletedAwareRepo(7)
	secured, inner := newAuditedL3App(t, repo)

	err := auditedSurfaceOf(t, secured).Purge(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, []int64{7}, inner.purged)
	require.Equal(t, 1, repo.includeHit)
}

// 查一条已删记录的审计轨迹是审计的主要用途，不能被软删过滤挡掉。
func TestAuditTrailResolvesDeletedBoundary(t *testing.T) {
	repo := newDeletedAwareRepo(7)
	secured, inner := newAuditedL3App(t, repo)

	_, err := auditedSurfaceOf(t, secured).AuditTrail(context.Background(), 7, 0, 10)

	require.NoError(t, err)
	require.Equal(t, []int64{7}, inner.trailed)
	require.Equal(t, 1, repo.includeHit)
}

// 装饰器的 EntityType 与仓储资源类型不一致时，约束恒匹配不到 → 构造期就该拒绝。
func TestNewRejectsEntityTypeMismatchingRepositoryKind(t *testing.T) {
	repo := newDeletedAwareRepo()
	plain, err := appcrud.NewApplication[*doc, int64](repo, nil, nil)
	require.NoError(t, err)
	authorizer, err := authscoped.NewAuthorizer(nil,
		authscoped.EvaluatorFunc(func(context.Context, string, []authscoped.Resource) (authscoped.Decision, error) {
			return authscoped.Decision{Effect: authscoped.EffectAllow}, nil
		}))
	require.NoError(t, err)

	_, err = secscoped.New[*doc, int64](plain, authorizer, secscoped.Config{
		EntityType: "documents", // 仓储声明的是 "document"
		Policy:     action.OperationPolicy{Create: "document:api:create"},
	})

	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}
