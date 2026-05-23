package audited

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	appcrud "gochen/app/crud"
	auth "gochen/auth"
	"gochen/contextx"
	"gochen/domain/access"
	"gochen/domain/audited"
	"gochen/domain/crud"
	"gochen/errors"
)

type auditedTestEntity struct {
	*audited.AuditedEntity[int64]
	Name   string `json:"name"`
	Status string `json:"status"`
}

type memAuditedRepo struct {
	items   map[int64]*auditedTestEntity
	creates int
	updates int
	deletes int
	purges  int
}

type memAuditedRepoWithListByIds struct {
	*memAuditedRepo
	getCalls       int
	listByIdsCalls int
}

func (r *memAuditedRepoWithListByIds) Get(ctx context.Context, id int64) (*auditedTestEntity, error) {
	r.getCalls++
	return r.memAuditedRepo.Get(ctx, id)
}

func (r *memAuditedRepoWithListByIds) ListByIds(ctx context.Context, ids []int64) ([]*auditedTestEntity, error) {
	r.listByIdsCalls++
	items := r.itemsFor(ctx)
	out := make([]*auditedTestEntity, 0, len(ids))
	for _, id := range ids {
		if e, ok := items[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

type memAuditedRepoWithPartialListByIds struct {
	*memAuditedRepoWithListByIds
	missing map[int64]struct{}
}

func (r *memAuditedRepoWithPartialListByIds) ListByIds(ctx context.Context, ids []int64) ([]*auditedTestEntity, error) {
	r.listByIdsCalls++
	items := r.itemsFor(ctx)
	out := make([]*auditedTestEntity, 0, len(ids))
	for _, id := range ids {
		if _, skip := r.missing[id]; skip {
			continue
		}
		if e, ok := items[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

type memAuditedRepoCountingGet struct {
	*memAuditedRepo
	getCalls int
}

func (r *memAuditedRepoCountingGet) Get(ctx context.Context, id int64) (*auditedTestEntity, error) {
	r.getCalls++
	return r.memAuditedRepo.Get(ctx, id)
}

type memAuditedConstrainedRepo struct {
	*memAuditedRepo
}

var _ access.IWriteConstraintRepository[*auditedTestEntity, int64] = (*memAuditedConstrainedRepo)(nil)

func (r *memAuditedConstrainedRepo) CreateWithConstraint(ctx context.Context, e *auditedTestEntity, constraint access.WriteConstraint) error {
	_ = constraint
	return r.Create(ctx, e)
}

func (r *memAuditedConstrainedRepo) UpdateWithConstraint(ctx context.Context, e *auditedTestEntity, constraint access.WriteConstraint) error {
	_ = constraint
	return r.Update(ctx, e)
}

func (r *memAuditedConstrainedRepo) DeleteWithConstraint(ctx context.Context, id int64, constraint access.WriteConstraint) error {
	_ = constraint
	return r.Delete(ctx, id)
}

// newMemAuditedRepo result：返回的实例（类型：*memAuditedRepo）。
//
// 返回：
func newMemAuditedRepo() *memAuditedRepo {
	return &memAuditedRepo{items: make(map[int64]*auditedTestEntity)}
}

type memAuditedRepoTx struct {
	items map[int64]*auditedTestEntity
}

type memAuditedRepoTxState struct {
	tx    *memAuditedRepoTx
	owned bool
}

type memAuditedRepoTxKey struct{}

// txFromContext ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - tx：返回结果（类型：*memAuditedRepoTx）
// - owned：是否满足条件
// - ok：是否满足条件
func (r *memAuditedRepo) txFromContext(ctx context.Context) (tx *memAuditedRepoTx, owned bool, ok bool) {
	if ctx == nil {
		return nil, false, false
	}
	state, ok := ctx.Value(memAuditedRepoTxKey{}).(memAuditedRepoTxState)
	if !ok || state.tx == nil {
		return nil, false, false
	}
	return state.tx, state.owned, true
}

// itemsFor ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - result：映射结果（类型：map[int64]*auditedTestEntity）
func (r *memAuditedRepo) itemsFor(ctx context.Context) map[int64]*auditedTestEntity {
	if tx, _, ok := r.txFromContext(ctx); ok {
		return tx.items
	}
	return r.items
}

// cloneAuditedTestEntity e：实体对象。
//
// 参数：
//
// 返回：
// - result：返回的实例（类型：*auditedTestEntity）
func cloneAuditedTestEntity(e *auditedTestEntity) *auditedTestEntity {
	if e == nil {
		return nil
	}
	cp := *e
	if e.AuditedEntity != nil {
		ae := *e.AuditedEntity
		cp.AuditedEntity = &ae
	}
	return &cp
}

var _ appcrud.ITransactional = (*memAuditedRepo)(nil)

func (r *memAuditedRepo) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return contextx.RunTxLifecycle(ctx, r, fn)
}

// BeginTx 开启事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：返回结果（类型：context.Context）
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if tx, _, ok := r.txFromContext(ctx); ok {
		// 已在事务中：复用外层事务（当前作用域不应再次提交/回滚）。
		txCtx := context.WithValue(ctx, memAuditedRepoTxKey{}, memAuditedRepoTxState{tx: tx, owned: false})
		return contextx.NewTxScope(txCtx, false)
	}

	snapshot := make(map[int64]*auditedTestEntity, len(r.items))
	for id, item := range r.items {
		snapshot[id] = cloneAuditedTestEntity(item)
	}
	tx := &memAuditedRepoTx{items: snapshot}
	txCtx := context.WithValue(ctx, memAuditedRepoTxKey{}, memAuditedRepoTxState{tx: tx, owned: true})
	return contextx.NewTxScope(txCtx, true)
}

// Commit 提交事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Commit(txScope contextx.TxScope) error {
	tx, owned, ok := r.txFromContext(txScope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !txScope.Owned() {
		return nil
	}
	r.items = tx.items
	return nil
}

// Rollback 回滚事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Rollback(txScope contextx.TxScope) error {
	_, owned, ok := r.txFromContext(txScope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !txScope.Owned() {
		return nil
	}
	// snapshot 丢弃即可（所有写入都发生在 tx.items）
	return nil
}

// Create 创建对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要创建的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Create(ctx context.Context, e *auditedTestEntity) error {
	_ = ctx
	r.creates++
	r.itemsFor(ctx)[e.GetID()] = e
	return nil
}

// Get 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*auditedTestEntity）
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Get(ctx context.Context, id int64) (*auditedTestEntity, error) {
	e, ok := r.itemsFor(ctx)[id]
	if !ok {
		return nil, errors.NewCode(errors.NotFound, "not found")
	}
	return e, nil
}

// GetWithDeleted 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：*auditedTestEntity）
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) GetWithDeleted(ctx context.Context, id int64) (*auditedTestEntity, error) {
	return r.Get(ctx, id)
}

// Update 更新对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要更新的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Update(ctx context.Context, e *auditedTestEntity) error {
	_ = ctx
	r.updates++
	r.itemsFor(ctx)[e.GetID()] = e
	return nil
}

// Delete 删除实体并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Delete(ctx context.Context, id int64) error {
	_ = ctx
	r.deletes++
	delete(r.itemsFor(ctx), id)
	return nil
}

// Purge ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - id：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Purge(ctx context.Context, id int64) error {
	_ = ctx
	r.purges++
	delete(r.itemsFor(ctx), id)
	return nil
}

// List 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：*auditedTestEntity）
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) List(ctx context.Context, offset, limit int) ([]*auditedTestEntity, error) {
	_ = offset
	_ = limit
	items := r.itemsFor(ctx)
	out := make([]*auditedTestEntity, 0, len(items))
	for _, e := range items {
		out = append(out, e)
	}
	return out, nil
}

// ListDeleted 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：*auditedTestEntity）
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) ListDeleted(ctx context.Context, offset, limit int) ([]*auditedTestEntity, error) {
	_ = offset
	_ = limit
	items := r.itemsFor(ctx)
	out := make([]*auditedTestEntity, 0, len(items))
	for _, e := range items {
		if e != nil && e.IsDeleted() {
			out = append(out, e)
		}
	}
	return out, nil
}

// Count 返回匹配条件的记录数。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Count(ctx context.Context) (int64, error) {
	return int64(len(r.itemsFor(ctx))), nil
}

// Exists 判断对象是否存在。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Exists(ctx context.Context, id int64) (bool, error) {
	_, ok := r.itemsFor(ctx)[id]
	return ok, nil
}

type recordingAuditStore struct {
	records []audited.AuditRecord
}

// SaveAuditRecord 写入一条审计记录。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - record：参数值（具体语义见函数上下文）（类型：audited.AuditRecord）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (s *recordingAuditStore) SaveAuditRecord(ctx context.Context, record audited.AuditRecord) (int64, error) {
	_ = ctx
	s.records = append(s.records, record)
	return int64(len(s.records)), nil
}

// ListAuditRecordsByEntity 从存储中查询数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - entityID：对象/实体标识
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：audited.AuditRecord）
// - err：错误信息（nil 表示成功）
func (s *recordingAuditStore) ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	_ = ctx
	var filtered []audited.AuditRecord
	for _, r := range s.records {
		if r.EntityID == entityID {
			filtered = append(filtered, r)
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return nil, nil
	}
	if limit <= 0 {
		limit = len(filtered)
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], nil
}

type recordingBatchAuditStore struct {
	records      []audited.AuditRecord
	singleCalls  int
	batchCalls   int
	lastBatchLen int
}

// SaveAuditRecord 写入一条审计记录。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - record：参数值（具体语义见函数上下文）（类型：audited.AuditRecord）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (s *recordingBatchAuditStore) SaveAuditRecord(ctx context.Context, record audited.AuditRecord) (int64, error) {
	_ = ctx
	s.singleCalls++
	s.records = append(s.records, record)
	return int64(len(s.records)), nil
}

// SaveAuditRecords ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - records：参数值（具体语义见函数上下文）（类型：[]audited.AuditRecord）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *recordingBatchAuditStore) SaveAuditRecords(ctx context.Context, records []audited.AuditRecord) error {
	_ = ctx
	s.batchCalls++
	s.lastBatchLen = len(records)
	s.records = append(s.records, records...)
	return nil
}

// ListAuditRecordsByEntity 从存储中查询数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - entityID：对象/实体标识
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：audited.AuditRecord）
// - err：错误信息（nil 表示成功）
func (s *recordingBatchAuditStore) ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	_ = ctx
	var filtered []audited.AuditRecord
	for _, r := range s.records {
		if r.EntityID == entityID {
			filtered = append(filtered, r)
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return nil, nil
	}
	if limit <= 0 {
		limit = len(filtered)
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], nil
}

type failingAuditStore struct {
	err error
}

// SaveAuditRecord 写入一条审计记录。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：audited.AuditRecord）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (s failingAuditStore) SaveAuditRecord(context.Context, audited.AuditRecord) (int64, error) {
	return 0, s.err
}

// ListAuditRecordsByEntity 从存储中查询数据。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - s：参数值（具体语义见函数上下文）（类型：string）
// - value：参数值（具体语义见函数上下文）（类型：int）
// - value：参数值（具体语义见函数上下文）（类型：int）
//
// 返回：
// - result1：列表结果（元素类型：audited.AuditRecord）
// - err：错误信息（nil 表示成功）
func (s failingAuditStore) ListAuditRecordsByEntity(context.Context, string, int, int) ([]audited.AuditRecord, error) {
	return nil, nil
}

// TestNewApplication_RequiresAuditStore 验证 NewApplication RequiresAuditStore。
func TestNewApplication_RequiresAuditStore(t *testing.T) {
	repo := newMemAuditedRepo()
	_, err := NewApplication(repo, nil, nil, nil)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
}

// TestAuditedApplication_Create_FailsFastWithoutOperator 验证 AuditedApplication Create FailsFastWithoutOperator。
func TestAuditedApplication_Create_FailsFastWithoutOperator(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(context.Background(), e); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
	if repo.creates != 0 {
		t.Fatalf("expected Create not persisted when operator missing, got creates=%d", repo.creates)
	}
}

// TestAuditedApplication_Create_WritesAuditRecord 验证 AuditedApplication Create WritesAuditRecord。
func TestAuditedApplication_Create_WritesAuditRecord(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if repo.creates != 1 {
		t.Fatalf("expected creates=1, got %d", repo.creates)
	}
	if len(store.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(store.records))
	}
	if store.records[0].Operation != audited.AuditOpCreate || store.records[0].Operator != "alice" || store.records[0].EntityID != "1" {
		t.Fatalf("unexpected audit record: %+v", store.records[0])
	}
}

func TestAuditedApplication_Create_PostCommitHookRunsAfterOuterCommit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app.SetHooks(&appcrud.Hooks[*auditedTestEntity, int64]{
		AfterCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			_, afterCreateSawCommitted = repo.items[entity.GetID()]
			return nil
		},
		PostCommitCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			_, postCommitSawCommitted = repo.items[entity.GetID()]
			return nil
		},
	})

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected after hook to run before outer transaction commit")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected post-commit hook to observe outer transaction commit")
	}
}

func TestAuditedApplication_Create_RollsBackWhenAfterCreateFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	app.SetHooks(&appcrud.Hooks[*auditedTestEntity, int64]{
		AfterCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			return errors.NewCode(errors.Internal, "after create failed")
		},
	})

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err == nil {
		t.Fatalf("expected create to fail when after hook fails")
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected entity not persisted after rollback, got err=%v", err)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record after rollback, got %d", len(store.records))
	}
}

// TestAuditedApplication_Create_RollsBackWhenAuditStoreFails 验证 AuditedApplication Create RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Create_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err == nil {
		t.Fatalf("expected create to fail when audit store fails")
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected entity not persisted when audit store fails, got err=%v", err)
	}
}

// TestAuditedApplication_Delete_SoftDeletesAndWritesAudit 验证 AuditedApplication Delete SoftDeletesAndWritesAudit。
func TestAuditedApplication_Delete_SoftDeletesAndWritesAudit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	repo.items[1] = e

	ctx, err := auth.WithOperator(context.Background(), "bob")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.Delete(ctx, 1); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if !got.IsDeleted() {
		t.Fatalf("expected entity to be soft-deleted")
	}
	if repo.updates != 1 {
		t.Fatalf("expected updates=1, got %d", repo.updates)
	}
	if len(store.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(store.records))
	}
	if store.records[0].Operation != audited.AuditOpDelete || store.records[0].Operator != "bob" {
		t.Fatalf("unexpected audit record: %+v", store.records[0])
	}
}

// TestAuditedApplication_GetAuditTrail_UsesStore 验证 AuditedApplication AuditTrail UsesStore。
func TestAuditedApplication_GetAuditTrail_UsesStore(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	if err := app.Create(ctx, e); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	records, err := app.AuditTrail(context.Background(), 1, 0, 100)
	if err != nil {
		t.Fatalf("get audit trail: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].EntityID != "1" {
		t.Fatalf("unexpected record: %+v", records[0])
	}

	// 缺失 store 的场景会在构造阶段被阻断；这里用编译期断言防止未来回退。
	if _, err := NewApplication(repo, nil, nil, nil); err == nil {
		t.Fatalf("expected constructor to fail without auditStore")
	}
}

// TestAuditedApplication_Purge_FailsFastWithoutOperator 验证 AuditedApplication Purge FailsFastWithoutOperator。
func TestAuditedApplication_Purge_FailsFastWithoutOperator(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	err = app.Purge(context.Background(), 1)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got %v", err)
	}
	if repo.purges != 0 {
		t.Fatalf("expected purge not executed when operator missing, got purges=%d", repo.purges)
	}
}

// TestAuditedApplication_Purge_DeletesAndWritesAudit 验证 AuditedApplication Purge DeletesAndWritesAudit。
func TestAuditedApplication_Purge_DeletesAndWritesAudit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	ctx, err := auth.WithOperator(context.Background(), "carol")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.Purge(ctx, 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if repo.purges != 1 {
		t.Fatalf("expected purges=1, got %d", repo.purges)
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected entity to be removed (NotFound), got err=%v", err)
	}
	if len(store.records) != 1 || store.records[0].Operation != audited.AuditOpDeleteHard || store.records[0].Operator != "carol" {
		t.Fatalf("unexpected audit record: %+v", store.records)
	}
}

// TestAuditedApplication_Update_RecordsFieldDiff 验证 AuditedApplication Update RecordsFieldDiff。
func TestAuditedApplication_Update_RecordsFieldDiff(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置实体
	e := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old_name",
		Status:        "pending",
	}
	repo.items[1] = e

	// 更新实体
	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updated := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "new_name",
		Status:        "active",
	}
	if err := app.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	// 验证审计记录
	if len(store.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(store.records))
	}
	rec := store.records[0]
	if rec.Operation != audited.AuditOpUpdate {
		t.Fatalf("expected UPDATE operation, got %s", rec.Operation)
	}
	if rec.Operator != "alice" {
		t.Fatalf("expected operator alice, got %s", rec.Operator)
	}

	// 验证 changes 包含 diff
	if rec.Changes == nil {
		t.Fatalf("expected changes to contain diff, got nil")
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(rec.Changes, &diffs); err != nil {
		t.Fatalf("failed to unmarshal changes: %v", err)
	}

	// 检查是否记录了 name 和 status 的变更
	diffMap := make(map[string]audited.FieldChange)
	for _, d := range diffs {
		diffMap[d.Field] = d
	}

	if nameDiff, ok := diffMap["name"]; !ok {
		t.Fatalf("expected diff for 'name' field")
	} else {
		if nameDiff.Old != "old_name" || nameDiff.New != "new_name" {
			t.Fatalf("unexpected name diff: old=%v, new=%v", nameDiff.Old, nameDiff.New)
		}
	}

	if statusDiff, ok := diffMap["status"]; !ok {
		t.Fatalf("expected diff for 'status' field")
	} else {
		if statusDiff.Old != "pending" || statusDiff.New != "active" {
			t.Fatalf("unexpected status diff: old=%v, new=%v", statusDiff.Old, statusDiff.New)
		}
	}
}

// TestAuditedApplication_UpdateAll_UsesBatchAuditStoreWhenAvailable 验证 AuditedApplication UpdateAll UsesBatchAuditStoreWhenAvailable。
func TestAuditedApplication_UpdateAll_UsesBatchAuditStoreWhenAvailable(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	writer := NewBatchWriter(app)

	// 预置实体
	repo.items[1] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "a",
		Status:        "p",
	}
	repo.items[2] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}},
		Name:          "b",
		Status:        "p",
	}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "a2", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "b2", Status: "active"},
	}
	if err := writer.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}

	if store.batchCalls != 1 {
		t.Fatalf("expected batchCalls=1, got %d", store.batchCalls)
	}
	if store.singleCalls != 0 {
		t.Fatalf("expected singleCalls=0, got %d", store.singleCalls)
	}
	if store.lastBatchLen != 2 {
		t.Fatalf("expected lastBatchLen=2, got %d", store.lastBatchLen)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(store.records))
	}
	for _, rec := range store.records {
		if rec.Operation != audited.AuditOpUpdate {
			t.Fatalf("unexpected op: %+v", rec)
		}
		if rec.Operator != "alice" {
			t.Fatalf("unexpected operator: %+v", rec)
		}
		if rec.Changes == nil {
			t.Fatalf("expected changes, got nil: %+v", rec)
		}
	}
}

func TestAuditedApplication_ConstrainedBatch_EnforcesMaxBatchSize(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingBatchAuditStore{}
	cfg := appcrud.DefaultServiceConfig()
	cfg.MaxBatchSize = 1
	app, err := NewApplication(repo, nil, cfg, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	writer := NewWriteConstraintWriter(app)
	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}

	entities := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2}}},
	}
	ids := []int64{1, 2}
	constraint := access.WriteConstraint{}

	if err := writer.CreateAllWithConstraint(ctx, entities, constraint); !errors.Is(err, errors.Validation) {
		t.Fatalf("expected CreateAllWithConstraint Validation, got %v", err)
	}
	if err := writer.UpdateAllWithConstraint(ctx, entities, constraint); !errors.Is(err, errors.Validation) {
		t.Fatalf("expected UpdateAllWithConstraint Validation, got %v", err)
	}
	if err := writer.DeleteAllWithConstraint(ctx, ids, constraint); !errors.Is(err, errors.Validation) {
		t.Fatalf("expected DeleteAllWithConstraint Validation, got %v", err)
	}
	if repo.creates != 0 || repo.updates != 0 || repo.deletes != 0 {
		t.Fatalf("expected repository untouched, creates=%d updates=%d deletes=%d", repo.creates, repo.updates, repo.deletes)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit records, got %d", len(store.records))
	}
}

func TestAuditedApplication_CreateAllWithConstraint_SplitsAfterBeforeHook(t *testing.T) {
	repo := &memAuditedConstrainedRepo{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	app.SetHooks(&appcrud.Hooks[*auditedTestEntity, int64]{
		BeforeCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			switch entity.Name {
			case "a":
				entity.SetID(101)
				entity.Version = 7
			case "b":
				entity.SetID(102)
				entity.Version = 8
			}
			return nil
		},
	})

	writer := NewWriteConstraintWriter(app)
	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{}, Name: "a"},
		{AuditedEntity: &audited.AuditedEntity[int64]{}, Name: "b"},
	}
	constraint := access.WriteConstraint{Resources: []access.ResourceConstraint{
		{ResourceID: "101", Revision: "7"},
		{ResourceID: "102", Revision: "8"},
	}}

	if err := writer.CreateAllWithConstraint(ctx, entities, constraint); err != nil {
		t.Fatalf("CreateAllWithConstraint: %v", err)
	}
	if ok, err := repo.Exists(context.Background(), 101); err != nil || !ok {
		t.Fatalf("expected entity 101 persisted, ok=%v err=%v", ok, err)
	}
	if ok, err := repo.Exists(context.Background(), 102); err != nil || !ok {
		t.Fatalf("expected entity 102 persisted, ok=%v err=%v", ok, err)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(store.records))
	}
}

func TestAuditedApplication_UpdateAllWithConstraint_SplitsAfterBeforeHook(t *testing.T) {
	repo := &memAuditedConstrainedRepo{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	app.SetHooks(&appcrud.Hooks[*auditedTestEntity, int64]{
		BeforeUpdate: func(ctx context.Context, entity *auditedTestEntity) error {
			switch entity.GetID() {
			case 1:
				entity.Version = 11
			case 2:
				entity.Version = 12
			}
			return nil
		},
	})

	repo.items[1] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old-a",
	}
	repo.items[2] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}},
		Name:          "old-b",
	}

	writer := NewWriteConstraintWriter(app)
	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "new-a"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "new-b"},
	}
	constraint := access.WriteConstraint{Resources: []access.ResourceConstraint{
		{ResourceID: "1", Revision: "11"},
		{ResourceID: "2", Revision: "12"},
	}}

	if err := writer.UpdateAllWithConstraint(ctx, updates, constraint); err != nil {
		t.Fatalf("UpdateAllWithConstraint: %v", err)
	}
	if got := repo.items[1].Version; got != 11 {
		t.Fatalf("expected entity 1 version 11, got %d", got)
	}
	if got := repo.items[2].Version; got != 12 {
		t.Fatalf("expected entity 2 version 12, got %d", got)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(store.records))
	}
}

func TestAuditedApplication_CreateAll_AfterHookRunsBeforeCommitAndPostCommitRunsAfterCommit(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app.SetHooks(&appcrud.Hooks[*auditedTestEntity, int64]{
		AfterCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			_, afterCreateSawCommitted = repo.items[entity.GetID()]
			return nil
		},
		PostCommitCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			_, postCommitSawCommitted = repo.items[entity.GetID()]
			return nil
		},
	})

	writer := NewBatchWriter(app)
	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2}}},
	}
	if err := writer.CreateAll(ctx, entities); err != nil {
		t.Fatalf("CreateAll: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected batch after hook to run before outer transaction commit")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected batch post-commit hook to observe outer transaction commit")
	}
}

func TestAuditedApplication_CreateAll_RollsBackWhenAfterCreateFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	var afterCalls int
	app.SetHooks(&appcrud.Hooks[*auditedTestEntity, int64]{
		AfterCreate: func(ctx context.Context, entity *auditedTestEntity) error {
			afterCalls++
			if entity.GetID() == 2 {
				return errors.NewCode(errors.Internal, "after create failed")
			}
			return nil
		},
	})

	writer := NewBatchWriter(app)
	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	entities := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2}}},
	}
	if err := writer.CreateAll(ctx, entities); err == nil {
		t.Fatalf("expected CreateAll to fail when after hook fails")
	}
	if afterCalls != 2 {
		t.Fatalf("expected after hook to run for each entity before failure, got %d", afterCalls)
	}
	if _, err := repo.Get(context.Background(), 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected first entity not persisted after rollback, got err=%v", err)
	}
	if _, err := repo.Get(context.Background(), 2); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected second entity not persisted after rollback, got err=%v", err)
	}
	if len(store.records) != 0 {
		t.Fatalf("expected no audit records after rollback, got %d", len(store.records))
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_UsesListByIds(t *testing.T) {
	repo := &memAuditedRepoWithListByIds{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	writer := NewBatchWriter(app)

	repo.items[1] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old",
		Status:        "pending",
	}
	repo.items[2] = &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}},
		Name:          "old",
		Status:        "pending",
	}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "new", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "new", Status: "active"},
	}
	if err := writer.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != 0 {
		t.Fatalf("expected Get not called when ListByIds available, got %d", repo.getCalls)
	}
	if repo.listByIdsCalls != 1 {
		t.Fatalf("expected ListByIds called once, got %d", repo.listByIdsCalls)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(store.records))
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_ChunksListByIds(t *testing.T) {
	repo := &memAuditedRepoWithListByIds{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	writer := NewBatchWriter(app)

	n := updateAllBeforeSnapshotChunkSize + 1
	for i := 0; i < n; i++ {
		id := int64(i + 1)
		repo.items[id] = &auditedTestEntity{
			AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: id, Version: 1}},
			Name:          fmt.Sprintf("old-%d", id),
			Status:        "pending",
		}
	}

	updates := make([]*auditedTestEntity, 0, n)
	for i := 0; i < n; i++ {
		id := int64(i + 1)
		updates = append(updates, &auditedTestEntity{
			AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: id, Version: 1}},
			Name:          fmt.Sprintf("new-%d", id),
			Status:        "active",
		})
	}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := writer.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != 0 {
		t.Fatalf("expected Get not called when ListByIds available, got %d", repo.getCalls)
	}
	if repo.listByIdsCalls != 2 {
		t.Fatalf("expected ListByIds called twice, got %d", repo.listByIdsCalls)
	}
	if len(store.records) != n {
		t.Fatalf("expected %d audit records, got %d", n, len(store.records))
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_FallsBackToGet(t *testing.T) {
	repo := &memAuditedRepoCountingGet{memAuditedRepo: newMemAuditedRepo()}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	writer := NewBatchWriter(app)

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "a", Status: "p"}
	repo.items[2] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "b", Status: "p"}
	repo.items[3] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 3, Version: 1}}, Name: "c", Status: "p"}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "a2", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "b2", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 3, Version: 1}}, Name: "c2", Status: "active"},
	}
	if err := writer.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != len(updates) {
		t.Fatalf("expected Get called %d times, got %d", len(updates), repo.getCalls)
	}
}

func TestAuditedApplication_UpdateAll_PrefetchBeforeSnapshots_FillsMissingListByIdsEntriesFromGet(t *testing.T) {
	repo := &memAuditedRepoWithPartialListByIds{
		memAuditedRepoWithListByIds: &memAuditedRepoWithListByIds{memAuditedRepo: newMemAuditedRepo()},
		missing:                     map[int64]struct{}{2: {}},
	}
	store := &recordingBatchAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}
	writer := NewBatchWriter(app)

	repo.items[1] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "old-1", Status: "pending"}
	repo.items[2] = &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "old-2", Status: "pending"}

	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updates := []*auditedTestEntity{
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}}, Name: "new-1", Status: "active"},
		{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 2, Version: 1}}, Name: "new-2", Status: "active"},
	}
	if err := writer.UpdateAll(ctx, updates); err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if repo.getCalls != 1 {
		t.Fatalf("expected Get called once for missing prefetch entity, got %d", repo.getCalls)
	}
	if repo.listByIdsCalls != 1 {
		t.Fatalf("expected ListByIds called once, got %d", repo.listByIdsCalls)
	}
	if len(store.records) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(store.records))
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(store.records[1].Changes, &diffs); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 field changes for fallback-loaded entity, got %d: %s", len(diffs), string(store.records[1].Changes))
	}
}

// TestComputeEntityDiff_ReturnsNilWhenNoChanges 验证 ComputeEntityDiff ReturnsNilWhenNoChanges。
func TestComputeEntityDiff_ReturnsNilWhenNoChanges(t *testing.T) {
	e := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "same",
		Status:        "same",
	}
	diff := computeEntityDiff(e, e)
	if diff != nil {
		t.Fatalf("expected nil diff for identical entities, got %s", string(diff))
	}
}

// TestComputeEntityDiff_SortedFieldOrder 验证 ComputeEntityDiff SortedFieldOrder。
func TestComputeEntityDiff_SortedFieldOrder(t *testing.T) {
	type testEntity struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		ZField string `json:"z_field"`
		AField string `json:"a_field"`
	}

	before := &testEntity{Name: "a", Status: "b", ZField: "z", AField: "a"}
	after := &testEntity{Name: "x", Status: "y", ZField: "w", AField: "v"}

	// 多次运行确保顺序稳定
	for i := 0; i < 10; i++ {
		diff := computeEntityDiff(before, after)
		if diff == nil {
			t.Fatalf("expected diff, got nil")
		}

		var diffs []audited.FieldChange
		if err := json.Unmarshal(diff, &diffs); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		// 验证按字母顺序排列：a_field, name, status, z_field
		expectedOrder := []string{"a_field", "name", "status", "z_field"}
		if len(diffs) != len(expectedOrder) {
			t.Fatalf("expected %d diffs, got %d", len(expectedOrder), len(diffs))
		}
		for j, expected := range expectedOrder {
			if diffs[j].Field != expected {
				t.Fatalf("iteration %d: expected field[%d]=%s, got %s", i, j, expected, diffs[j].Field)
			}
		}
	}
}

// TestComputeEntityDiff_PreservesNumberType 验证 ComputeEntityDiff PreservesNumberType。
func TestComputeEntityDiff_PreservesNumberType(t *testing.T) {
	type testEntity struct {
		Count int64 `json:"count"`
	}

	before := &testEntity{Count: 100}
	after := &testEntity{Count: 200}

	diff := computeEntityDiff(before, after)
	if diff == nil {
		t.Fatalf("expected diff, got nil")
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(diff, &diffs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(diffs) != 1 || diffs[0].Field != "count" {
		t.Fatalf("unexpected diffs: %+v", diffs)
	}

	// 验证 Old/New 保留为整数表示（不会有小数点）
	diffJSON := string(diff)
	if strings.Contains(diffJSON, "100.") || strings.Contains(diffJSON, "200.") {
		t.Fatalf("expected integer representation without decimal, got: %s", diffJSON)
	}
	// json.Number 在最终 Marshal 时会保持原始数值表示
	if !strings.Contains(diffJSON, `"old":100`) || !strings.Contains(diffJSON, `"new":200`) {
		t.Fatalf("expected integer values in diff, got: %s", diffJSON)
	}
}

// TestComputeEntityDiff_FiltersFrameworkFields 验证 ComputeEntityDiff FiltersFrameworkFields。
func TestComputeEntityDiff_FiltersFrameworkFields(t *testing.T) {
	before := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "old",
		Status:        "pending",
	}
	after := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 2}}, // version 变了
		Name:          "new",
		Status:        "pending",
	}

	diff := computeEntityDiff(before, after)
	if diff == nil {
		t.Fatalf("expected diff, got nil")
	}

	var diffs []audited.FieldChange
	if err := json.Unmarshal(diff, &diffs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 只应包含 name 变更，version 应被过滤
	for _, d := range diffs {
		if isAuditIgnoredField(d.Field) {
			t.Fatalf("framework field %q should be filtered from diff", d.Field)
		}
	}

	// 验证 name 变更存在
	found := false
	for _, d := range diffs {
		if d.Field == "name" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'name' field in diff, got: %+v", diffs)
	}
}

// ============================================================================
// 边界场景测试（审阅报告 5.2 建议补充）
// ============================================================================

// TestAuditedApplication_Delete_AlreadyDeletedReturnsConflict 验证 AuditedApplication Delete AlreadyDeletedReturnsConflict。
func TestAuditedApplication_Delete_AlreadyDeletedReturnsConflict(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条已软删的数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	_ = e.SoftDeleteBy("alice", time.Now())
	repo.items[1] = e

	ctx, err := auth.WithOperator(context.Background(), "bob")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	err = app.Delete(ctx, 1)
	if !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected Conflict error for already deleted entity, got %v", err)
	}
	// 审计记录不应写入
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record when delete fails, got %d", len(store.records))
	}
}

// TestAuditedApplication_Restore_NotDeletedReturnsConflict 验证 AuditedApplication Restore NotDeletedReturnsConflict。
func TestAuditedApplication_Restore_NotDeletedReturnsConflict(t *testing.T) {
	repo := newMemAuditedRepo()
	store := &recordingAuditStore{}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条未删除的数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	repo.items[1] = e

	err = app.Restore(context.Background(), 1, "bob")
	if !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected Conflict error for non-deleted entity restore, got %v", err)
	}
	// 审计记录不应写入
	if len(store.records) != 0 {
		t.Fatalf("expected no audit record when restore fails, got %d", len(store.records))
	}
}

// TestAuditedApplication_Update_RollsBackWhenAuditStoreFails 验证 AuditedApplication Update RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Update_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置实体
	original := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "original_name",
		Status:        "pending",
	}
	repo.items[1] = cloneAuditedTestEntity(original)

	// 尝试更新
	ctx, err := auth.WithOperator(context.Background(), "alice")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	updated := &auditedTestEntity{
		AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1, Version: 1}},
		Name:          "updated_name",
		Status:        "active",
	}
	if err := app.Update(ctx, updated); err == nil {
		t.Fatalf("expected update to fail when audit store fails")
	}

	// 验证实体未被修改（事务已回滚）
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after failed update: %v", err)
	}
	if got.Name != "original_name" {
		t.Fatalf("expected entity to be rolled back, got name=%s", got.Name)
	}
}

// TestAuditedApplication_Delete_RollsBackWhenAuditStoreFails 验证 AuditedApplication Delete RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Delete_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置实体
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	repo.items[1] = e

	ctx, err := auth.WithOperator(context.Background(), "bob")
	if err != nil {
		t.Fatalf("WithOperator returned error: %v", err)
	}
	if err := app.Delete(ctx, 1); err == nil {
		t.Fatalf("expected delete to fail when audit store fails")
	}

	// 验证实体未被软删（事务已回滚）
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after failed delete: %v", err)
	}
	if got.IsDeleted() {
		t.Fatalf("expected entity to not be deleted after rollback")
	}
}

// TestAuditedApplication_Restore_RollsBackWhenAuditStoreFails 验证 AuditedApplication Restore RollsBackWhenAuditStoreFails。
func TestAuditedApplication_Restore_RollsBackWhenAuditStoreFails(t *testing.T) {
	repo := newMemAuditedRepo()
	store := failingAuditStore{err: errors.NewCode(errors.Internal, "audit store down")}
	app, err := NewApplication(repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	// 预置一条已软删的数据
	e := &auditedTestEntity{AuditedEntity: &audited.AuditedEntity[int64]{Entity: crud.Entity[int64]{ID: 1}}}
	_ = e.SoftDeleteBy("alice", time.Now())
	repo.items[1] = e

	if err := app.Restore(context.Background(), 1, "bob"); err == nil {
		t.Fatalf("expected restore to fail when audit store fails")
	}

	// 验证实体仍为已删除状态（事务已回滚）
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get after failed restore: %v", err)
	}
	if !got.IsDeleted() {
		t.Fatalf("expected entity to remain deleted after rollback")
	}
}
