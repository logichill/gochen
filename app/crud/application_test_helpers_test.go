package crud

import (
	"context"

	"gochen/app/query"
	"gochen/db/tx"
	"gochen/domain"
	"gochen/errors"
	"gochen/validate"
)

type testEntity struct {
	id      int64
	version uint64
}

type selfValidatingEntity struct {
	id       int64
	order    *[]string
	validate error
}

func (e *selfValidatingEntity) GetID() int64 { return e.id }

func (e *selfValidatingEntity) GetVersion() uint64 { return 0 }

func (e *selfValidatingEntity) Validate() error {
	*e.order = append(*e.order, "domain")
	return e.validate
}

type orderedValidator struct {
	order *[]string
	err   error
}

func (v orderedValidator) Validate(any) error {
	*v.order = append(*v.order, "application")
	return v.err
}

// GetID 返回当前值。
//
// 返回：
// - result：数量/计数
func (e testEntity) GetID() int64 { return e.id }

// GetVersion 返回当前值。
//
// 返回：
// - result：数量/计数
func (e testEntity) GetVersion() uint64 { return e.version }

var _ domain.IEntity[int64] = testEntity{}

type recordingRepo struct {
	lastListOffset int
	lastListLimit  int
	count          int64
	items          []testEntity
}

// Create 创建对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要创建的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Create(ctx context.Context, e testEntity) error { return nil }

// Update 更新对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要更新的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Update(ctx context.Context, e testEntity) error { return nil }

// Delete 删除实体并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Delete(ctx context.Context, id int64) error { return nil }

// Get 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Get(ctx context.Context, id int64) (testEntity, error) {
	return testEntity{}, errors.New("not implemented")
}

// List 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) List(ctx context.Context, offset, limit int) ([]testEntity, error) {
	r.lastListOffset = offset
	r.lastListLimit = limit
	return r.items, nil
}

// Count 返回匹配条件的记录数。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Count(ctx context.Context) (int64, error) { return r.count, nil }

// Exists 判断对象是否存在。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Exists(ctx context.Context, id int64) (bool, error) {
	return false, nil
}

type queryRepo struct {
	recordingRepo
}

var _ query.IQueryableRepository[testEntity] = (*queryRepo)(nil)

// Query 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：列表结果（元素类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *queryRepo) Query(ctx context.Context, opts query.QueryOptions) ([]testEntity, error) {
	return r.items, nil
}

// QueryOne 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *queryRepo) QueryOne(ctx context.Context, opts query.QueryOptions) (testEntity, error) {
	return testEntity{}, errors.New("not implemented")
}

// QueryCount 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *queryRepo) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	return r.count, nil
}

type capturingQueryRepo struct {
	recordingRepo
	lastQueryOpts query.QueryOptions
	lastCountOpts query.QueryOptions
}

var _ query.IQueryableRepository[testEntity] = (*capturingQueryRepo)(nil)

// Query 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：列表结果（元素类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *capturingQueryRepo) Query(ctx context.Context, opts query.QueryOptions) ([]testEntity, error) {
	r.lastQueryOpts = opts
	return r.items, nil
}

// QueryOne 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *capturingQueryRepo) QueryOne(ctx context.Context, opts query.QueryOptions) (testEntity, error) {
	return testEntity{}, errors.New("not implemented")
}

// QueryCount 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *capturingQueryRepo) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	r.lastCountOpts = opts
	return r.count, nil
}

type trackingValidator struct {
	err    error
	called int
}

func singleQueryFilter(field string, expr query.QueryExpr) query.QueryFilters {
	var filters query.QueryFilters
	return filters.Append(field, expr)
}

// Validate 校验输入是否满足约束。
//
// 参数：
// - v：参数值（具体语义见函数上下文）（类型：any）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (t *trackingValidator) Validate(any) error {
	t.called++
	return t.err
}

var _ validate.IValidator = (*trackingValidator)(nil)

// batchTestRepo 用于批量操作测试的仓储
type batchTestRepo struct {
	recordingRepo
	existingIDs map[int64]bool
	creates     []int64
	updates     []int64
	deletes     []int64
}

type batchTestRepoTxKey struct{}

type batchTestRepoTxState struct {
	existingIDs map[int64]bool
	creates     []int64
	updates     []int64
	deletes     []int64
}

// newBatchTestRepo result：返回的实例（类型：*batchTestRepo）。
//
// 返回：
func newBatchTestRepo() *batchTestRepo {
	return &batchTestRepo{
		existingIDs: make(map[int64]bool),
		creates:     make([]int64, 0),
		updates:     make([]int64, 0),
		deletes:     make([]int64, 0),
	}
}

// txState ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - result1：返回结果（类型：*batchTestRepoTxState）
// - result2：是否满足条件
func (r *batchTestRepo) txState(ctx context.Context) (*batchTestRepoTxState, bool) {
	v := ctx.Value(batchTestRepoTxKey{})
	if v == nil {
		return nil, false
	}
	tx, ok := v.(*batchTestRepoTxState)
	return tx, ok && tx != nil
}

// Create 创建对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要创建的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Create(ctx context.Context, e testEntity) error {
	if tx, ok := r.txState(ctx); ok {
		tx.creates = append(tx.creates, e.id)
		tx.existingIDs[e.id] = true
		return nil
	}
	r.creates = append(r.creates, e.id)
	r.existingIDs[e.id] = true
	return nil
}

func (r *batchTestRepo) Update(ctx context.Context, e testEntity) error {
	if tx, ok := r.txState(ctx); ok {
		if !tx.existingIDs[e.id] {
			return errors.NewCode(errors.NotFound, "entity not found")
		}
		tx.updates = append(tx.updates, e.id)
		return nil
	}
	if !r.existingIDs[e.id] {
		return errors.NewCode(errors.NotFound, "entity not found")
	}
	r.updates = append(r.updates, e.id)
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
func (r *batchTestRepo) Delete(ctx context.Context, id int64) error {
	if tx, ok := r.txState(ctx); ok {
		if !tx.existingIDs[id] {
			return errors.NewCode(errors.NotFound, "entity not found")
		}
		tx.deletes = append(tx.deletes, id)
		delete(tx.existingIDs, id)
		return nil
	}
	if !r.existingIDs[id] {
		return errors.NewCode(errors.NotFound, "entity not found")
	}
	r.deletes = append(r.deletes, id)
	delete(r.existingIDs, id)
	return nil
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
func (r *batchTestRepo) Exists(ctx context.Context, id int64) (bool, error) {
	if tx, ok := r.txState(ctx); ok {
		return tx.existingIDs[id], nil
	}
	return r.existingIDs[id], nil
}

// Get 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Get(ctx context.Context, id int64) (testEntity, error) {
	if tx, ok := r.txState(ctx); ok {
		if !tx.existingIDs[id] {
			return testEntity{}, errors.NewCode(errors.NotFound, "entity not found")
		}
		return testEntity{id: id}, nil
	}
	if !r.existingIDs[id] {
		return testEntity{}, errors.NewCode(errors.NotFound, "entity not found")
	}
	return testEntity{id: id}, nil
}

// BeginTx 开启事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：返回结果（类型：context.Context）
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) BeginTx(ctx context.Context) (tx.TxScope, error) {
	existingIDs := make(map[int64]bool, len(r.existingIDs))
	for k, v := range r.existingIDs {
		existingIDs[k] = v
	}
	creates := append([]int64(nil), r.creates...)
	txState := &batchTestRepoTxState{
		existingIDs: existingIDs,
		creates:     creates,
		updates:     append([]int64(nil), r.updates...),
		deletes:     append([]int64(nil), r.deletes...),
	}
	txCtx := context.WithValue(ctx, batchTestRepoTxKey{}, txState)
	return tx.NewTxScope(txCtx, true)
}

func (r *batchTestRepo) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

// Commit 提交事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Commit(txScope tx.TxScope) error {
	tx, ok := r.txState(txScope.Context())
	if !ok {
		return errors.NewCode(errors.Internal, "missing tx state")
	}
	r.existingIDs = tx.existingIDs
	r.creates = tx.creates
	r.updates = tx.updates
	r.deletes = tx.deletes
	return nil
}

// Rollback 回滚事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Rollback(txScope tx.TxScope) error {
	_ = txScope
	return nil
}

// selectiveValidator 选择性校验器：对特定 ID 的实体返回错误
type selectiveValidator struct {
	failIDs map[int64]bool
}

// Validate 校验输入是否满足约束。
//
// 参数：
// - entity：实体对象
//
// 返回：
// - err：错误信息（nil 表示成功）
func (v *selectiveValidator) Validate(entity any) error {
	if e, ok := entity.(testEntity); ok {
		if v.failIDs[e.id] {
			return errors.New("validation failed for entity")
		}
	}
	return nil
}
