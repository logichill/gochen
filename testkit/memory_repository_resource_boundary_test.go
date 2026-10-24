package testkit

import (
	"context"
	"testing"
	"time"

	"gochen/auth/scoped"
	"gochen/domain/crud"
)

// boundaryTestEntity 覆盖 ITenantEntity + ISoftDeletable + 边界字段的最小实体。
type boundaryTestEntity struct {
	crud.TenantEntity[int64]
	ManagedScopeID int64  `db:"managed_scope_id"`
	OwnerID        string `db:"owner_id"`
	deleted        bool
}

func (e *boundaryTestEntity) GetVersion() uint64 { return 7 }
func (e *boundaryTestEntity) IsDeleted() bool    { return e.deleted }
func (e *boundaryTestEntity) GetDeletedAt() *time.Time {
	if !e.deleted {
		return nil
	}
	at := time.Unix(1700000000, 0)
	return &at
}
func (e *boundaryTestEntity) SoftDelete(at time.Time) error { e.deleted = true; return nil }
func (e *boundaryTestEntity) Restore() error                { e.deleted = false; return nil }

func boundaryFixture(t *testing.T) *MemoryRepository[*boundaryTestEntity, int64] {
	t.Helper()
	repo := NewMemoryRepository[*boundaryTestEntity, int64](nil)
	e := &boundaryTestEntity{ManagedScopeID: 5, OwnerID: "u1"}
	e.SetID(1)
	e.SetTenantID("t1")
	if err := repo.Create(context.Background(), e); err != nil {
		t.Fatalf("create: %v", err)
	}
	return repo
}

func TestResourceBoundary_Probe(t *testing.T) {
	repo := boundaryFixture(t)
	var probe scoped.IResourceBoundaryReader[int64] = repo
	res, err := probe.ResolveResourceByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.ID != "1" || res.OwnerID != "u1" || res.Revision != "7" || res.ManagedScopeID != 5 || res.TenantID != "t1" {
		t.Fatalf("unexpected resource: %+v", res)
	}
}

// 普通实体（仅嵌 crud.Entity，不带租户）同样必须解析出带 ID 的资源边界：
// GetID 以具体类型实现时曾因 interface{ GetID() any } 断言不命中而静默丢失。
func TestResourceBoundary_PlainEntityKeepsID(t *testing.T) {
	repo := NewMemoryRepository[*crud.Entity[int64], int64](nil)
	if err := repo.Create(context.Background(), &crud.Entity[int64]{ID: 7, Version: 3}); err != nil {
		t.Fatalf("create: %v", err)
	}
	var probe scoped.IResourceBoundaryReader[int64] = repo
	res, err := probe.ResolveResourceByID(context.Background(), 7)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.ID != "7" || res.Revision != "3" {
		t.Fatalf("unexpected resource: %+v", res)
	}
}

// boundaryUntaggedEntity 完全不打 gorm/db/json tag：缩写字段 OwnerID 与
// ManagedScopeID 的列名只能由 snake_case 字段名转换得出。P2#25（0260d854）
// 回归：自制 snake_case 把缩写词渲染为 owner_i_d / managed_scope_i_d，
// 与生产 casecodec 口径（owner_id / managed_scope_id）分叉，边界资源字段恒空。
type boundaryUntaggedEntity struct {
	crud.Entity[int64]
	ManagedScopeID int64
	OwnerID        string
}

// P2#25 回归：未打 tag 的缩写字段（OwnerID/ManagedScopeID）必须经与生产
// 同源的 casecodec snake_case 转换解析出边界列，而不是因口径分叉恒空。
func TestResourceBoundary_UntaggedAbbreviatedFieldsResolve(t *testing.T) {
	repo := NewMemoryRepository[*boundaryUntaggedEntity, int64](nil)
	e := &boundaryUntaggedEntity{ManagedScopeID: 9, OwnerID: "u9"}
	e.SetID(1)
	if err := repo.Create(context.Background(), e); err != nil {
		t.Fatalf("create: %v", err)
	}

	var probe scoped.IResourceBoundaryReader[int64] = repo
	res, err := probe.ResolveResourceByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.OwnerID != "u9" {
		t.Fatalf("untagged OwnerID must resolve via casecodec snake_case (owner_id), got %q", res.OwnerID)
	}
	if res.ManagedScopeID != 9 {
		t.Fatalf("untagged ManagedScopeID must resolve via casecodec snake_case (managed_scope_id), got %d", res.ManagedScopeID)
	}

	second := &boundaryUntaggedEntity{ManagedScopeID: 10, OwnerID: "u10"}
	second.SetID(2)
	if err := repo.Create(context.Background(), second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	var batch scoped.IBatchResourceBoundaryReader[int64] = repo
	resources, err := batch.ResolveResourcesByID(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatalf("batch resolve: %v", err)
	}
	if len(resources) != 2 || resources[0].OwnerID != "u9" || resources[1].OwnerID != "u10" {
		t.Fatalf("unexpected batch resources: %+v", resources)
	}
	if resources[0].ManagedScopeID != 9 || resources[1].ManagedScopeID != 10 {
		t.Fatalf("unexpected batch managed scopes: %+v", resources)
	}
}

// memoryToSnakeCase 与生产 casecodec 口径一致（P2#25 回归守护）：
// 缩写词边界按词元切分，OwnerID→owner_id、ManagedScopeID→managed_scope_id。
func TestMemoryToSnakeCase_MatchesCasecodec(t *testing.T) {
	cases := map[string]string{
		"OwnerID":        "owner_id",
		"ManagedScopeID": "managed_scope_id",
		"ID":             "id",
		"UserID":         "user_id",
		"HTTPServer":     "http_server",
		"AlreadySnake":   "already_snake",
		"Version":        "version",
	}
	for in, want := range cases {
		if got := memoryToSnakeCase(in); got != want {
			t.Fatalf("memoryToSnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResourceBoundary_Batch(t *testing.T) {
	repo := boundaryFixture(t)
	second := &boundaryTestEntity{ManagedScopeID: 6, OwnerID: "u2"}
	second.SetID(2)
	second.SetTenantID("t1")
	if err := repo.Create(context.Background(), second); err != nil {
		t.Fatalf("create: %v", err)
	}
	var probe scoped.IBatchResourceBoundaryReader[int64] = repo
	resources, err := probe.ResolveResourcesByID(context.Background(), []int64{2, 1})
	if err != nil {
		t.Fatalf("batch resolve: %v", err)
	}
	if len(resources) != 2 || resources[0].OwnerID != "u2" || resources[1].OwnerID != "u1" {
		t.Fatalf("unexpected resources: %+v", resources)
	}
	if _, err := probe.ResolveResourcesByID(context.Background(), []int64{1, 99}); err == nil {
		t.Fatalf("expected NotFound for missing id")
	}
}

func TestResourceBoundary_Deleted(t *testing.T) {
	repo := boundaryFixture(t)
	ctx := context.Background()
	if err := repo.withWriteState(ctx, func(state *memoryState[*boundaryTestEntity, int64]) error {
		state.entities[1].deleted = true
		return nil
	}); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	var reader scoped.IResourceBoundaryReader[int64] = repo
	if _, err := reader.ResolveResourceByID(ctx, 1); err == nil {
		t.Fatalf("expected NotFound for soft-deleted entity via normal probe")
	}
	var deletedReader scoped.IDeletedResourceBoundaryReader[int64] = repo
	res, err := deletedReader.ResolveResourceByIDIncludingDeleted(ctx, 1)
	if err != nil {
		t.Fatalf("resolve including deleted: %v", err)
	}
	if res.OwnerID != "u1" {
		t.Fatalf("unexpected resource: %+v", res)
	}
}

// cycleSelfEntity 自引用实体（树形建模常见）：managed_scope_id 不在顶层，
// 边界解析必须遍历整个类型图（含 *cycleSelfEntity 自环）后安全返回零值，
// 而不是无限递归栈溢出。
type cycleSelfEntity struct {
	crud.Entity[int64]
	OwnerID string `json:"owner_id"`
	Parent  *cycleSelfEntity
	Child   *cycleSelfEntity
}

// cycleAEntity / cycleBEntity 互引用实体：A ↔ B 构成类型环。
type cycleAEntity struct {
	crud.Entity[int64]
	Next *cycleBEntity
}

type cycleBEntity struct {
	crud.Entity[int64]
	Prev    *cycleAEntity
	OwnerID string `json:"owner_id"`
}

// P2#18 回归：自引用实体解析边界不 panic、不栈溢出。
func TestResourceBoundary_SelfReferentialEntityDoesNotOverflow(t *testing.T) {
	repo := NewMemoryRepository[*cycleSelfEntity, int64](nil)
	root := &cycleSelfEntity{OwnerID: "u1"}
	root.SetID(1)
	root.Parent = root // 值环（类型图本身已构成环，此处双保险）
	root.Child = &cycleSelfEntity{}
	if err := repo.Create(context.Background(), root); err != nil {
		t.Fatalf("create: %v", err)
	}

	var probe scoped.IResourceBoundaryReader[int64] = repo
	res, err := probe.ResolveResourceByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.ID != "1" || res.OwnerID != "u1" {
		t.Fatalf("unexpected resource: %+v", res)
	}
	if res.ManagedScopeID != 0 {
		t.Fatalf("missing column must resolve to zero, got %d", res.ManagedScopeID)
	}

	var batch scoped.IBatchResourceBoundaryReader[int64] = repo
	resources, err := batch.ResolveResourcesByID(context.Background(), []int64{1})
	if err != nil {
		t.Fatalf("batch resolve: %v", err)
	}
	if len(resources) != 1 || resources[0].OwnerID != "u1" {
		t.Fatalf("unexpected batch resources: %+v", resources)
	}

	var deletedReader scoped.IDeletedResourceBoundaryReader[int64] = repo
	if _, err := deletedReader.ResolveResourceByIDIncludingDeleted(context.Background(), 1); err != nil {
		t.Fatalf("resolve including deleted: %v", err)
	}
}

// P2#18 回归：互引用实体（A↔B 类型环）解析边界不 panic、不栈溢出；
// 经非 nil 指针字段嵌套命中的字符串列仍能正确读出。
func TestResourceBoundary_MutuallyReferentialEntitiesDoNotOverflow(t *testing.T) {
	repo := NewMemoryRepository[*cycleAEntity, int64](nil)
	entity := &cycleAEntity{Next: &cycleBEntity{OwnerID: "u2"}}
	entity.SetID(1)
	if err := repo.Create(context.Background(), entity); err != nil {
		t.Fatalf("create: %v", err)
	}

	var probe scoped.IResourceBoundaryReader[int64] = repo
	res, err := probe.ResolveResourceByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.ID != "1" {
		t.Fatalf("unexpected resource: %+v", res)
	}
	// owner_id 位于 A.Next（*cycleBEntity）内：非 nil 指针路径应读出嵌套值
	if res.OwnerID != "u2" {
		t.Fatalf("nested owner_id via pointer field must resolve, got %q", res.OwnerID)
	}

	// nil 指针路径必须返回零值而不是 panic（reflect FieldByIndex 守卫）
	orphan := &cycleAEntity{}
	orphan.SetID(2)
	if err := repo.Create(context.Background(), orphan); err != nil {
		t.Fatalf("create orphan: %v", err)
	}
	res, err = probe.ResolveResourceByID(context.Background(), 2)
	if err != nil {
		t.Fatalf("resolve orphan: %v", err)
	}
	if res.OwnerID != "" || res.ManagedScopeID != 0 {
		t.Fatalf("nil pointer path must resolve to zero values, got %+v", res)
	}
}
