package testkit

import (
	"context"
	"sync"

	"gochen/contextx"
	"gochen/db/tx"
	"gochen/domain"
	"gochen/errors"
)

type memoryTransaction[T domain.IEntity[ID], ID comparable] struct {
	mu           sync.Mutex
	repository   *MemoryRepository[T, ID]
	state        memoryState[T, ID]
	baseRevision uint64
	closed       bool
	// chainHost 表示本事务是否是当前事务链路的宿主：BeginTx 时 ctx 尚未携带
	// 事务生命周期状态（contextx.TxLifecycleFromContext 未命中）。当本仓储事务
	// 作为独立资源事务嵌套在宿主事务链路（其他仓储的 WithinTx）内时，
	// chainHost=false：提交/回滚只作用于本仓储的内存状态，不得触碰宿主共享的
	// after-commit 回调状态（不 MarkTxCommitted、不 ResetAfterCommit），
	// 回调随宿主事务统一提交或撤销。
	chainHost bool
}

type memoryTransactionContext[T domain.IEntity[ID], ID comparable] struct {
	transaction *memoryTransaction[T, ID]
	owned       bool
}

type memoryTransactionKey[T domain.IEntity[ID], ID comparable] struct{}

// WithinTx 在内存快照事务中执行 fn；并发提交冲突返回 errors.Concurrency。
func (r *MemoryRepository[T, ID]) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return tx.RunTxLifecycle(ctx, r, fn)
}

// BeginTx 创建内存快照事务。
//
// 嵌套语义与生产 SQL 适配器（gochen-runtime db/orm/repo/transaction.go）一致：
// 同一仓储事务内再次 BeginTx 复用外层事务，内层 Commit/Rollback 均为 no-op，
// 内层已写入的数据留在外层事务状态中随外层提交/回滚（内层回滚不撤销写入，
// 由调用方通过外层回滚整体丢弃）。
func (r *MemoryRepository[T, ID]) BeginTx(ctx context.Context) (tx.TxScope, error) {
	if err := validateContext(ctx); err != nil {
		return tx.TxScope{}, err
	}
	if transactionState, ok := r.transactionFromContext(ctx); ok {
		transaction := transactionState.transaction
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return tx.TxScope{}, errors.NewCode(errors.FailedPrecondition, "memory transaction already closed")
		}
		txCtx := context.WithValue(ctx, memoryTransactionKey[T, ID]{}, memoryTransactionContext[T, ID]{
			transaction: transaction,
			owned:       false,
		})
		return tx.NewTxScope(txCtx, false)
	}

	// ctx 已携带事务生命周期状态时，本事务是链路内的独立资源事务而非宿主：
	// 作用域以非宿主（owner=false）身份挂入链路，使 RunTxLifecycle 的回调
	// 清理按内层水位线语义执行，宿主回调不受本资源事务提交/回滚影响。
	_, hasChainLifecycle := contextx.TxLifecycleFromContext(ctx)
	transaction := &memoryTransaction[T, ID]{
		repository: r,
		chainHost:  !hasChainLifecycle,
	}
	r.mu.RLock()
	transaction.state = cloneMemoryState(r.state)
	transaction.baseRevision = r.revision
	r.mu.RUnlock()
	txCtx := context.WithValue(ctx, memoryTransactionKey[T, ID]{}, memoryTransactionContext[T, ID]{
		transaction: transaction,
		owned:       true,
	})
	return tx.NewTxScope(txCtx, !hasChainLifecycle)
}

// Commit 提交内存快照事务。
func (r *MemoryRepository[T, ID]) Commit(scope tx.TxScope) error {
	transactionState, ok := r.transactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "memory transaction not started")
	}
	transaction := transactionState.transaction
	if !transactionState.owned {
		// 嵌套内层作用域：与生产 SQL 适配器一致，提交为 no-op，
		// 本层写入继续留在外层事务状态中随外层提交。
		return nil
	}
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.closed {
		return errors.NewCode(errors.FailedPrecondition, "memory transaction already closed")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revision != transaction.baseRevision {
		return errors.NewCode(errors.Concurrency, "memory transaction commit conflict")
	}
	r.state = cloneMemoryState(transaction.state)
	r.revision++
	transaction.closed = true
	// 仅链路宿主事务提交后封存 after-commit 回调批次；链路内的独立资源事务
	// 提交不得封存宿主批次，否则宿主后续注册回调会被误拒。
	if transaction.chainHost {
		contextx.MarkTxCommitted(scope.Context())
	}
	return nil
}

// Rollback 丢弃内存快照事务。
func (r *MemoryRepository[T, ID]) Rollback(scope tx.TxScope) error {
	transactionState, ok := r.transactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "memory transaction not started")
	}
	transaction := transactionState.transaction
	if !transactionState.owned {
		// 嵌套内层作用域：与生产 SQL 适配器一致，回滚为 no-op（不撤销本层
		// 写入）。本层注册的 after-commit 回调由 RunTxLifecycle 的
		// ResetAfterCommit 水位线截断负责撤销，此处不再触碰回调状态。
		return nil
	}
	transaction.mu.Lock()
	transaction.closed = true
	transaction.mu.Unlock()
	// 仅链路宿主事务回滚时清空/封存回调状态；链路内的独立资源事务回滚
	// 不得清空宿主已注册的回调（回调撤销由 RunTxLifecycle 水位线语义处理）。
	if transaction.chainHost {
		contextx.ResetAfterCommit(scope.Context())
	}
	return nil
}

func (r *MemoryRepository[T, ID]) transactionFromContext(ctx context.Context) (memoryTransactionContext[T, ID], bool) {
	var zero memoryTransactionContext[T, ID]
	if ctx == nil {
		return zero, false
	}
	state, ok := ctx.Value(memoryTransactionKey[T, ID]{}).(memoryTransactionContext[T, ID])
	if !ok || state.transaction == nil || state.transaction.repository != r {
		return zero, false
	}
	return state, true
}
