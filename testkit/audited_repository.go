package testkit

import (
	"context"
	"strings"
	"sync"

	appcrud "gochen/app/crud"
	"gochen/db/tx"
	"gochen/domain/audited"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/gen"
)

// AuditedMemoryRepository 在 MemoryRepository 之上提供软删除查询与事务化审计记录。
//
// 实体变更和审计记录在同一个乐观内存事务中提交；事务期间若仓储被其他调用修改，提交会返回
// errors.Concurrency。该实现适合 audited application 的示例和单元测试。
type AuditedMemoryRepository[T audited.IAuditedEntity[ID], ID comparable] struct {
	*MemoryRepository[T, ID]

	auditMu       sync.RWMutex
	audits        map[string][]audited.AuditRecord
	nextAuditID   int64
	auditRevision uint64
}

type auditedTransaction[T audited.IAuditedEntity[ID], ID comparable] struct {
	mu                sync.Mutex
	repository        *AuditedMemoryRepository[T, ID]
	entityTransaction *memoryTransaction[T, ID]
	audits            map[string][]audited.AuditRecord
	nextAuditID       int64
	baseAuditRevision uint64
	closed            bool
}

type auditedTransactionContext[T audited.IAuditedEntity[ID], ID comparable] struct {
	transaction *auditedTransaction[T, ID]
	owned       bool
}

type auditedTransactionKey[T audited.IAuditedEntity[ID], ID comparable] struct{}

// NewAuditedMemoryRepository 创建审计型内存仓储。
func NewAuditedMemoryRepository[T audited.IAuditedEntity[ID], ID comparable](generator gen.IGenerator[ID]) *AuditedMemoryRepository[T, ID] {
	return &AuditedMemoryRepository[T, ID]{
		MemoryRepository: NewMemoryRepository[T, ID](generator),
		audits:           make(map[string][]audited.AuditRecord),
		nextAuditID:      1,
	}
}

// Get 返回未删除实体；软删除实体按未找到处理。
func (r *AuditedMemoryRepository[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	entity, err := r.MemoryRepository.Get(ctx, id)
	if err != nil {
		return entity, err
	}
	if entity.IsDeleted() {
		var zero T
		return zero, entityNotFound(id)
	}
	return entity, nil
}

// GetWithDeleted 返回实体，无论其是否已软删除。
func (r *AuditedMemoryRepository[T, ID]) GetWithDeleted(ctx context.Context, id ID) (T, error) {
	return r.MemoryRepository.Get(ctx, id)
}

// List 按创建顺序返回未删除实体。
func (r *AuditedMemoryRepository[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	var result []T
	err := r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		visible := make([]T, 0, len(state.order))
		for _, id := range state.order {
			entity := state.entities[id]
			if !entity.IsDeleted() {
				visible = append(visible, shallowClone(entity))
			}
		}
		start, end := pageBounds(len(visible), offset, limit)
		result = append([]T(nil), visible[start:end]...)
		return nil
	})
	return result, err
}

// ListDeleted 按创建顺序返回已软删除实体。
func (r *AuditedMemoryRepository[T, ID]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	var result []T
	err := r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		deleted := make([]T, 0)
		for _, id := range state.order {
			entity := state.entities[id]
			if entity.IsDeleted() {
				deleted = append(deleted, shallowClone(entity))
			}
		}
		start, end := pageBounds(len(deleted), offset, limit)
		result = append([]T(nil), deleted[start:end]...)
		return nil
	})
	return result, err
}

// Count 返回未删除实体数量。
func (r *AuditedMemoryRepository[T, ID]) Count(ctx context.Context) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	var count int64
	err := r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		for _, entity := range state.entities {
			if !entity.IsDeleted() {
				count++
			}
		}
		return nil
	})
	return count, err
}

// Exists 判断未删除实体是否存在。
func (r *AuditedMemoryRepository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	entity, err := r.MemoryRepository.Get(ctx, id)
	if errors.Is(err, errors.NotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !entity.IsDeleted(), nil
}

// SaveAuditRecord 保存一条审计记录并返回生成的记录 ID。
func (r *AuditedMemoryRepository[T, ID]) SaveAuditRecord(ctx context.Context, record audited.AuditRecord) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	if err := validateAuditRecord(record); err != nil {
		return 0, err
	}
	if transaction, _, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return 0, errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		return appendAuditRecord(transaction.audits, &transaction.nextAuditID, record), nil
	}
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	id := appendAuditRecord(r.audits, &r.nextAuditID, record)
	r.auditRevision++
	return id, nil
}

// SaveAuditRecords 原子保存一批审计记录。
func (r *AuditedMemoryRepository[T, ID]) SaveAuditRecords(ctx context.Context, records []audited.AuditRecord) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	for _, record := range records {
		if err := validateAuditRecord(record); err != nil {
			return err
		}
	}
	if len(records) == 0 {
		return nil
	}
	if transaction, _, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		for _, record := range records {
			appendAuditRecord(transaction.audits, &transaction.nextAuditID, record)
		}
		return nil
	}
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	for _, record := range records {
		appendAuditRecord(r.audits, &r.nextAuditID, record)
	}
	r.auditRevision++
	return nil
}

// ListAuditRecordsByEntity 按实体 ID 返回审计记录分页结果。
func (r *AuditedMemoryRepository[T, ID]) ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, errors.NewCode(errors.InvalidInput, "audit entity ID is required")
	}
	var records []audited.AuditRecord
	if transaction, _, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return nil, errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		records = cloneAuditRecords(transaction.audits[entityID])
	} else {
		r.auditMu.RLock()
		records = cloneAuditRecords(r.audits[entityID])
		r.auditMu.RUnlock()
	}
	start, end := pageBounds(len(records), offset, limit)
	return records[start:end], nil
}

// WithinTx 在同一个内存事务中执行实体与审计写入。
func (r *AuditedMemoryRepository[T, ID]) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return tx.RunTxLifecycle(ctx, r, fn)
}

// BeginTx 创建同时覆盖实体和审计记录的内存快照事务。
func (r *AuditedMemoryRepository[T, ID]) BeginTx(ctx context.Context) (tx.TxScope, error) {
	if err := validateContext(ctx); err != nil {
		return tx.TxScope{}, err
	}
	if transaction, _, ok := r.auditedTransactionFromContext(ctx); ok {
		txCtx := context.WithValue(ctx, auditedTransactionKey[T, ID]{}, auditedTransactionContext[T, ID]{
			transaction: transaction,
			owned:       false,
		})
		txCtx = context.WithValue(txCtx, memoryTransactionKey[T, ID]{}, memoryTransactionContext[T, ID]{
			transaction: transaction.entityTransaction,
			owned:       false,
		})
		return tx.NewTxScope(txCtx, false)
	}
	if _, _, ok := r.transactionFromContext(ctx); ok {
		return tx.TxScope{}, errors.NewCode(errors.FailedPrecondition, "cannot start audited transaction inside entity-only transaction")
	}

	r.mu.RLock()
	r.auditMu.RLock()
	entityTransaction := &memoryTransaction[T, ID]{
		repository:   r.MemoryRepository,
		state:        cloneMemoryState(r.state),
		baseRevision: r.revision,
	}
	transaction := &auditedTransaction[T, ID]{
		repository:        r,
		entityTransaction: entityTransaction,
		audits:            cloneAuditMap(r.audits),
		nextAuditID:       r.nextAuditID,
		baseAuditRevision: r.auditRevision,
	}
	r.auditMu.RUnlock()
	r.mu.RUnlock()

	txCtx := context.WithValue(ctx, memoryTransactionKey[T, ID]{}, memoryTransactionContext[T, ID]{
		transaction: entityTransaction,
		owned:       true,
	})
	txCtx = context.WithValue(txCtx, auditedTransactionKey[T, ID]{}, auditedTransactionContext[T, ID]{
		transaction: transaction,
		owned:       true,
	})
	return tx.NewTxScope(txCtx, true)
}

// Commit 原子提交实体与审计快照。
func (r *AuditedMemoryRepository[T, ID]) Commit(scope tx.TxScope) error {
	transaction, owned, ok := r.auditedTransactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "audited memory transaction not started")
	}
	if !owned || !scope.Owned() {
		return nil
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	transaction.entityTransaction.mu.Lock()
	defer transaction.entityTransaction.mu.Unlock()
	if transaction.closed || transaction.entityTransaction.closed {
		return errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
	}

	r.mu.Lock()
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	defer r.mu.Unlock()
	if r.revision != transaction.entityTransaction.baseRevision || r.auditRevision != transaction.baseAuditRevision {
		return errors.NewCode(errors.Concurrency, "audited memory transaction commit conflict")
	}
	r.state = cloneMemoryState(transaction.entityTransaction.state)
	r.audits = cloneAuditMap(transaction.audits)
	r.nextAuditID = transaction.nextAuditID
	r.revision++
	r.auditRevision++
	transaction.closed = true
	transaction.entityTransaction.closed = true
	return nil
}

// Rollback 丢弃实体与审计快照。
func (r *AuditedMemoryRepository[T, ID]) Rollback(scope tx.TxScope) error {
	transaction, owned, ok := r.auditedTransactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "audited memory transaction not started")
	}
	if !owned || !scope.Owned() {
		return nil
	}
	transaction.mu.Lock()
	transaction.entityTransaction.mu.Lock()
	transaction.closed = true
	transaction.entityTransaction.closed = true
	transaction.entityTransaction.mu.Unlock()
	transaction.mu.Unlock()
	return nil
}

func (r *AuditedMemoryRepository[T, ID]) auditedTransactionFromContext(ctx context.Context) (*auditedTransaction[T, ID], bool, bool) {
	if ctx == nil {
		return nil, false, false
	}
	state, ok := ctx.Value(auditedTransactionKey[T, ID]{}).(auditedTransactionContext[T, ID])
	if !ok || state.transaction == nil || state.transaction.repository != r {
		return nil, false, false
	}
	return state.transaction, state.owned, true
}

func validateAuditRecord(record audited.AuditRecord) error {
	if strings.TrimSpace(record.EntityID) == "" {
		return errors.NewCode(errors.InvalidInput, "audit entity ID is required")
	}
	if strings.TrimSpace(string(record.Operation)) == "" {
		return errors.NewCode(errors.InvalidInput, "audit operation is required")
	}
	if strings.TrimSpace(record.Operator) == "" {
		return errors.NewCode(errors.InvalidInput, "audit operator is required")
	}
	return nil
}

func appendAuditRecord(records map[string][]audited.AuditRecord, nextID *int64, record audited.AuditRecord) int64 {
	record.EntityID = strings.TrimSpace(record.EntityID)
	record.ID = *nextID
	*nextID = *nextID + 1
	records[record.EntityID] = append(records[record.EntityID], cloneAuditRecord(record))
	return record.ID
}

func cloneAuditMap(records map[string][]audited.AuditRecord) map[string][]audited.AuditRecord {
	cloned := make(map[string][]audited.AuditRecord, len(records))
	for entityID, entityRecords := range records {
		cloned[entityID] = cloneAuditRecords(entityRecords)
	}
	return cloned
}

func cloneAuditRecords(records []audited.AuditRecord) []audited.AuditRecord {
	cloned := make([]audited.AuditRecord, len(records))
	for i, record := range records {
		cloned[i] = cloneAuditRecord(record)
	}
	return cloned
}

func cloneAuditRecord(record audited.AuditRecord) audited.AuditRecord {
	record.Changes = append([]byte(nil), record.Changes...)
	record.Metadata = append([]byte(nil), record.Metadata...)
	return record
}

var (
	_ appcrud.ITransactional                                                = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ domaincrud.IRepository[audited.IAuditedEntity[int64], int64]          = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ domaincrud.IQueryRepository[audited.IAuditedEntity[int64], int64]     = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IRestoreRepository[audited.IAuditedEntity[int64], int64]      = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IDeletedQueryRepository[audited.IAuditedEntity[int64], int64] = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IAuditStore                                                   = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IBatchAuditStore                                              = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
)
