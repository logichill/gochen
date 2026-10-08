package audited

import (
	"context"

	appcrud "gochen/app/crud"

	"gochen/db/tx"
	"gochen/domain/audited"

	"gochen/errors"
)

type auditedTestEntity struct {
	*audited.AuditedEntity[int64]
	Name     string `json:"name"`
	Status   string `json:"status"`
	Snapshot any    `json:"snapshot,omitempty"`
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

// memAuditedRepoFilteringDeleted 模拟真实 Repo.Get 的软删过滤行为（deleted_at IS NULL）。
type memAuditedRepoFilteringDeleted struct {
	*memAuditedRepo
}

func (r *memAuditedRepoFilteringDeleted) Get(ctx context.Context, id int64) (*auditedTestEntity, error) {
	e, ok := r.itemsFor(ctx)[id]
	if !ok || e.IsDeleted() {
		return nil, errors.NewCode(errors.NotFound, "not found")
	}
	return e, nil
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
	return tx.RunTxLifecycle(ctx, r, fn)
}

// BeginTx 开启事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：返回结果（类型：context.Context）
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) BeginTx(ctx context.Context) (tx.TxScope, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	if currentTx, _, ok := r.txFromContext(ctx); ok {
		// 已在事务中：复用外层事务（当前作用域不应再次提交/回滚）。
		txCtx := context.WithValue(ctx, memAuditedRepoTxKey{}, memAuditedRepoTxState{tx: currentTx, owned: false})
		return tx.NewTxScope(txCtx, false)
	}

	snapshot := make(map[int64]*auditedTestEntity, len(r.items))
	for id, item := range r.items {
		snapshot[id] = cloneAuditedTestEntity(item)
	}
	txObj := &memAuditedRepoTx{items: snapshot}
	txCtx := context.WithValue(ctx, memAuditedRepoTxKey{}, memAuditedRepoTxState{tx: txObj, owned: true})
	return tx.NewTxScope(txCtx, true)
}

// Commit 提交事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *memAuditedRepo) Commit(txScope tx.TxScope) error {
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
func (r *memAuditedRepo) Rollback(txScope tx.TxScope) error {
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

type invalidDiffJSONEntity struct{}

func (invalidDiffJSONEntity) MarshalJSON() ([]byte, error) {
	return []byte(`[]`), nil
}
