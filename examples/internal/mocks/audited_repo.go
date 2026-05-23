// Package mocks 提供审计型内存仓储（示例/测试用）
package mocks

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"

	appcrud "gochen/app/crud"
	"gochen/contextx"
	"gochen/domain/audited"
	"gochen/errors"
)

// MockAuditedRepository 基于内存的审计型仓储实现。
// - 复用 GenericMockRepository 提供的基础 CRUD 行为；
// - 实现 audited.IAuditStore，用于保存与查询审计记录。
type MockAuditedRepository[T audited.IAuditedEntity[int64]] struct {
	*GenericMockRepository[T]
	audits map[int64][]audited.AuditRecord
	mu     sync.Mutex
	nextID int64
}

type mockAuditedTx[T audited.IAuditedEntity[int64]] struct {
	entities     map[int64]T
	nextEntityID int64
	audits       map[int64][]audited.AuditRecord
	nextAuditID  int64
}

type mockAuditedTxState[T audited.IAuditedEntity[int64]] struct {
	tx    *mockAuditedTx[T]
	owned bool
}

type mockAuditedTxKey struct{}

// txFromContext 从上下文里提取当前 mock 审计事务状态。
func (r *MockAuditedRepository[T]) txFromContext(ctx context.Context) (tx *mockAuditedTx[T], owned bool, ok bool) {
	if ctx == nil {
		return nil, false, false
	}
	state, ok := ctx.Value(mockAuditedTxKey{}).(mockAuditedTxState[T])
	if !ok || state.tx == nil {
		return nil, false, false
	}
	return state.tx, state.owned, true
}

// cloneEntity 对指针结构体实体做一层浅拷贝。
func cloneEntity[T audited.IAuditedEntity[int64]](entity T) T {
	if any(entity) == nil {
		var zero T
		return zero
	}
	v := reflect.ValueOf(entity)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return entity
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return entity
	}
	cp := reflect.New(elem.Type())
	cp.Elem().Set(elem)
	out, ok := cp.Interface().(T)
	if !ok {
		return entity
	}
	return out
}

// cloneAuditMap 复制审计记录映射。
func cloneAuditMap(src map[int64][]audited.AuditRecord) map[int64][]audited.AuditRecord {
	if src == nil {
		return map[int64][]audited.AuditRecord{}
	}
	out := make(map[int64][]audited.AuditRecord, len(src))
	for k, v := range src {
		cp := make([]audited.AuditRecord, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

var _ appcrud.ITransactional = (*MockAuditedRepository[audited.IAuditedEntity[int64]])(nil)

func (r *MockAuditedRepository[T]) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return contextx.RunTxLifecycle(ctx, r, fn)
}

// BeginTx 创建一个基于 map 快照的内存事务作用域。
func (r *MockAuditedRepository[T]) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	if ctx == nil {
		return contextx.TxScope{}, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if tx, _, ok := r.txFromContext(ctx); ok {
		txCtx := context.WithValue(ctx, mockAuditedTxKey{}, mockAuditedTxState[T]{tx: tx, owned: false})
		return contextx.NewTxScope(txCtx, false)
	}

	// 说明：示例用内存事务，只保证“map 级别”的原子提交；实体内部的深拷贝仅做浅层 struct copy。
	r.GenericMockRepository.mu.RLock()
	r.mu.Lock()
	entities := make(map[int64]T, len(r.GenericMockRepository.entities))
	for id, e := range r.GenericMockRepository.entities {
		entities[id] = cloneEntity(e)
	}
	tx := &mockAuditedTx[T]{
		entities:     entities,
		nextEntityID: r.GenericMockRepository.nextID,
		audits:       cloneAuditMap(r.audits),
		nextAuditID:  r.nextID,
	}
	r.mu.Unlock()
	r.GenericMockRepository.mu.RUnlock()

	txCtx := context.WithValue(ctx, mockAuditedTxKey{}, mockAuditedTxState[T]{tx: tx, owned: true})
	return contextx.NewTxScope(txCtx, true)
}

// Commit 把事务快照回写到仓储主存储。
func (r *MockAuditedRepository[T]) Commit(txScope contextx.TxScope) error {
	tx, owned, ok := r.txFromContext(txScope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !txScope.Owned() {
		return nil
	}

	r.GenericMockRepository.mu.Lock()
	r.mu.Lock()
	r.GenericMockRepository.entities = tx.entities
	r.GenericMockRepository.nextID = tx.nextEntityID
	r.audits = tx.audits
	r.nextID = tx.nextAuditID
	r.mu.Unlock()
	r.GenericMockRepository.mu.Unlock()
	return nil
}

// Rollback 在内存事务实现里只需要丢弃事务快照。
func (r *MockAuditedRepository[T]) Rollback(txScope contextx.TxScope) error {
	_, owned, ok := r.txFromContext(txScope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "transaction not started")
	}
	if !owned || !txScope.Owned() {
		return nil
	}
	return nil
}

// NewMockAuditedRepository 创建一个带审计记录存储的内存仓储。
func NewMockAuditedRepository[T audited.IAuditedEntity[int64]]() *MockAuditedRepository[T] {
	return &MockAuditedRepository[T]{
		GenericMockRepository: NewGenericMockRepository[T](),
		audits:                map[int64][]audited.AuditRecord{},
	}
}

// Create 创建实体；在事务中会直接写入事务快照。
func (r *MockAuditedRepository[T]) Create(ctx context.Context, entity T) error {
	if tx, _, ok := r.txFromContext(ctx); ok {
		// 尽力模拟自增 ID（仅当实体提供 SetID 时才会写入）。
		if entity.GetID() == 0 {
			if withID, ok := any(entity).(interface{ SetID(int64) }); ok {
				withID.SetID(tx.nextEntityID)
			}
		}
		if entity.GetID() >= tx.nextEntityID {
			tx.nextEntityID = entity.GetID() + 1
		} else {
			tx.nextEntityID++
		}
		tx.entities[entity.GetID()] = entity
		return nil
	}
	return r.GenericMockRepository.Create(ctx, entity)
}

// Get 按 ID 读取实体，并优先读取事务快照。
func (r *MockAuditedRepository[T]) Get(ctx context.Context, id int64) (T, error) {
	if tx, _, ok := r.txFromContext(ctx); ok {
		entity, exists := tx.entities[id]
		if !exists {
			var zero T
			return zero, errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", id))
		}
		return entity, nil
	}
	return r.GenericMockRepository.Get(ctx, id)
}

// GetWithDeleted 在示例仓储里与 Get 共用同一套读取逻辑。
func (r *MockAuditedRepository[T]) GetWithDeleted(ctx context.Context, id int64) (T, error) {
	return r.Get(ctx, id)
}

// Update 更新实体；在事务中会直接写入事务快照。
func (r *MockAuditedRepository[T]) Update(ctx context.Context, entity T) error {
	if tx, _, ok := r.txFromContext(ctx); ok {
		if _, exists := tx.entities[entity.GetID()]; !exists {
			return errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", entity.GetID()))
		}
		tx.entities[entity.GetID()] = entity
		return nil
	}
	return r.GenericMockRepository.Update(ctx, entity)
}

// Delete 删除实体；在事务中会直接修改事务快照。
func (r *MockAuditedRepository[T]) Delete(ctx context.Context, id int64) error {
	if tx, _, ok := r.txFromContext(ctx); ok {
		if _, exists := tx.entities[id]; !exists {
			return errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", id))
		}
		delete(tx.entities, id)
		return nil
	}
	return r.GenericMockRepository.Delete(ctx, id)
}

// List 返回实体的简单分页视图，并优先读取事务快照。
func (r *MockAuditedRepository[T]) List(ctx context.Context, offset, limit int) ([]T, error) {
	if tx, _, ok := r.txFromContext(ctx); ok {
		entities := make([]T, 0, len(tx.entities))
		for _, entity := range tx.entities {
			entities = append(entities, entity)
		}
		if offset >= len(entities) {
			return []T{}, nil
		}
		end := offset + limit
		if end > len(entities) {
			end = len(entities)
		}
		return entities[offset:end], nil
	}
	return r.GenericMockRepository.List(ctx, offset, limit)
}

// ListDeleted 返回当前被标记为已删除的实体分页列表。
func (r *MockAuditedRepository[T]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}

	var deleted []T
	if tx, _, ok := r.txFromContext(ctx); ok {
		deleted = make([]T, 0, len(tx.entities))
		for _, entity := range tx.entities {
			if entity.IsDeleted() {
				deleted = append(deleted, entity)
			}
		}
	} else {
		r.GenericMockRepository.mu.RLock()
		deleted = make([]T, 0, len(r.GenericMockRepository.entities))
		for _, entity := range r.GenericMockRepository.entities {
			if entity.IsDeleted() {
				deleted = append(deleted, entity)
			}
		}
		r.GenericMockRepository.mu.RUnlock()
	}

	if offset >= len(deleted) {
		return []T{}, nil
	}
	end := offset + limit
	if end > len(deleted) {
		end = len(deleted)
	}
	return deleted[offset:end], nil
}

// Count 返回当前可见实体数量。
func (r *MockAuditedRepository[T]) Count(ctx context.Context) (int64, error) {
	if tx, _, ok := r.txFromContext(ctx); ok {
		return int64(len(tx.entities)), nil
	}
	return r.GenericMockRepository.Count(ctx)
}

// Exists 判断指定 ID 的实体是否存在。
func (r *MockAuditedRepository[T]) Exists(ctx context.Context, id int64) (bool, error) {
	if tx, _, ok := r.txFromContext(ctx); ok {
		_, exists := tx.entities[id]
		return exists, nil
	}
	return r.GenericMockRepository.Exists(ctx, id)
}

// SaveAuditRecord 保存审计记录。
func (r *MockAuditedRepository[T]) SaveAuditRecord(ctx context.Context, rec audited.AuditRecord) (int64, error) {
	id, err := strconv.ParseInt(rec.EntityID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid entity id for audit record: %s", rec.EntityID)
	}

	if tx, _, ok := r.txFromContext(ctx); ok {
		tx.nextAuditID++
		rec.ID = tx.nextAuditID
		tx.audits[id] = append(tx.audits[id], rec)
		return rec.ID, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	rec.ID = r.nextID
	r.audits[id] = append(r.audits[id], rec)
	return rec.ID, nil
}

// ListAuditRecordsByEntity 按实体 ID 返回审计记录分页列表。
func (r *MockAuditedRepository[T]) ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	id, err := strconv.ParseInt(entityID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid entity id for audit query: %s", entityID)
	}
	recs := r.audits[id]
	if tx, _, ok := r.txFromContext(ctx); ok {
		recs = tx.audits[id]
	}
	if offset >= len(recs) {
		return []audited.AuditRecord{}, nil
	}
	end := offset + limit
	if end > len(recs) {
		end = len(recs)
	}
	return recs[offset:end], nil
}
