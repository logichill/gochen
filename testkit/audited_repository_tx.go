package testkit

import (
	"context"
	domain "gochen/domain"
	"sync"

	"gochen/contextx"
	"gochen/db/tx"

	"gochen/errors"
)

type auditedTransaction[T domain.IEntity[ID], ID comparable] struct {
	mu                sync.Mutex
	repository        *AuditedMemoryRepository[T, ID]
	entityTransaction *memoryTransaction[T, ID]
	audits            map[string][]memoryAuditRecord
	nextAuditID       int64
	baseAuditRevision uint64
	closed            bool
	// chainHost 语义与 memoryTransaction.chainHost 一致：本审计事务是否是当前
	// 事务链路的宿主。链路内的独立资源事务提交/回滚不得触碰宿主共享的
	// after-commit 回调状态。
	chainHost bool
}

type auditedTransactionContext[T domain.IEntity[ID], ID comparable] struct {
	transaction *auditedTransaction[T, ID]
	owned       bool
}

type auditedTransactionKey[T domain.IEntity[ID], ID comparable] struct{}

// WithinTx 在同一个内存事务中执行实体与审计写入。
func (r *AuditedMemoryRepository[T, ID]) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return tx.RunTxLifecycle(ctx, r, fn)
}

// BeginTx 创建同时覆盖实体和审计记录的内存快照事务。
//
// 嵌套语义与生产 SQL 适配器（gochen-runtime db/orm/repo/transaction.go）一致：
// 同一审计事务内再次 BeginTx 复用外层事务，内层 Commit/Rollback 均为 no-op，
// 内层写入的实体与审计记录留在外层事务状态中随外层提交/回滚。
func (r *AuditedMemoryRepository[T, ID]) BeginTx(ctx context.Context) (tx.TxScope, error) {
	if err := validateContext(ctx); err != nil {
		return tx.TxScope{}, err
	}
	if transactionState, ok := r.auditedTransactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		transaction.entityTransaction.mu.Lock()
		defer transaction.entityTransaction.mu.Unlock()
		defer transaction.mu.Unlock()
		if transaction.closed || transaction.entityTransaction.closed {
			return tx.TxScope{}, errors.NewCode(errors.FailedPrecondition, "audited memory transaction already closed")
		}
		txCtx := context.WithValue(ctx, memoryTransactionKey[T, ID]{}, memoryTransactionContext[T, ID]{
			transaction: transaction.entityTransaction,
			owned:       false,
		})
		txCtx = context.WithValue(txCtx, auditedTransactionKey[T, ID]{}, auditedTransactionContext[T, ID]{
			transaction: transaction,
			owned:       false,
		})
		return tx.NewTxScope(txCtx, false)
	}
	if _, ok := r.transactionFromContext(ctx); ok {
		return tx.TxScope{}, errors.NewCode(errors.FailedPrecondition, "cannot start audited transaction inside entity-only transaction")
	}

	// 与 MemoryRepository.BeginTx 一致：ctx 已携带事务生命周期状态时，
	// 本事务以非宿主（owner=false）身份挂入链路，不触碰宿主回调状态。
	_, hasChainLifecycle := contextx.TxLifecycleFromContext(ctx)
	chainHost := !hasChainLifecycle

	r.mu.RLock()
	r.auditMu.RLock()
	entityTransaction := &memoryTransaction[T, ID]{
		repository: r.MemoryRepository,
		chainHost:  chainHost,
		state:      cloneMemoryState(r.state),
	}
	entityTransaction.baseRevision = r.revision
	transaction := &auditedTransaction[T, ID]{
		repository:        r,
		entityTransaction: entityTransaction,
		audits:            cloneAuditMap(r.audits),
		nextAuditID:       r.nextAuditID,
		baseAuditRevision: r.auditRevision,
		chainHost:         chainHost,
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
	return tx.NewTxScope(txCtx, chainHost)
}

// Commit 原子提交实体与审计快照。
func (r *AuditedMemoryRepository[T, ID]) Commit(scope tx.TxScope) error {
	transactionState, ok := r.auditedTransactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "audited memory transaction not started")
	}
	transaction := transactionState.transaction
	if !transactionState.owned {
		// 嵌套内层作用域：与生产 SQL 适配器一致，提交为 no-op。
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
	// 仅链路宿主事务提交后封存 after-commit 回调批次（见 memory_repository_tx.go）。
	if transaction.chainHost {
		contextx.MarkTxCommitted(scope.Context())
	}
	return nil
}

// Rollback 丢弃实体与审计快照。
func (r *AuditedMemoryRepository[T, ID]) Rollback(scope tx.TxScope) error {
	transactionState, ok := r.auditedTransactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "audited memory transaction not started")
	}
	transaction := transactionState.transaction
	if !transactionState.owned {
		// 嵌套内层作用域：与生产 SQL 适配器一致，回滚为 no-op；
		// 回调撤销由 RunTxLifecycle 的水位线语义负责。
		return nil
	}
	transaction.mu.Lock()
	transaction.entityTransaction.mu.Lock()
	transaction.closed = true
	transaction.entityTransaction.closed = true
	transaction.entityTransaction.mu.Unlock()
	transaction.mu.Unlock()
	// 仅链路宿主事务回滚时清空/封存回调状态（见 memory_repository_tx.go）。
	if transaction.chainHost {
		contextx.ResetAfterCommit(scope.Context())
	}
	return nil
}

func (r *AuditedMemoryRepository[T, ID]) auditedTransactionFromContext(ctx context.Context) (auditedTransactionContext[T, ID], bool) {
	var zero auditedTransactionContext[T, ID]
	if ctx == nil {
		return zero, false
	}
	state, ok := ctx.Value(auditedTransactionKey[T, ID]{}).(auditedTransactionContext[T, ID])
	if !ok || state.transaction == nil || state.transaction.repository != r {
		return zero, false
	}
	return state, true
}
