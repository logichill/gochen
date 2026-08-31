package testkit

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	appcrud "gochen/app/crud"
	"gochen/auth/scoped"
	"gochen/db/tx"
	"gochen/domain"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/gen"
)

// MemoryRepository 是支持 CRUD、查询、批量操作和内存事务的泛型测试仓储。
//
// 实体写入和读取时会对“指向 struct 的指针”做一层浅拷贝；包含 map、slice、指针字段的实体若需
// 完全隔离，应在测试模型中自行复制这些引用字段。本实现通过插入顺序提供稳定的 List 结果。
type MemoryRepository[T domain.IEntity[ID], ID comparable] struct {
	mu        sync.RWMutex
	state     memoryState[T, ID]
	revision  uint64
	generator gen.IGenerator[ID]
	isolated  bool
	// isolationResolve 为 nil 时隔离键取自 contextx.TenantID（见 memory_repository_isolation.go）。
	isolationResolve IsolationResolveFunc
	// crossIsolationReads 记录本仓储是否有被开闸跨隔离只读的资格
	// （见 memory_repository_cross_isolation.go）。
	crossIsolationReads bool
	crossIsolationAudit ICrossIsolationAudit
	// scopeEntityType 非空表示已显式声明 L3 范围能力（见 memory_repository_scoped.go）。
	scopeEntityType string
}

type memoryState[T domain.IEntity[ID], ID comparable] struct {
	entities map[ID]T
	order    []ID
}

type memoryTransaction[T domain.IEntity[ID], ID comparable] struct {
	mu           sync.Mutex
	repository   *MemoryRepository[T, ID]
	state        memoryState[T, ID]
	baseRevision uint64
	closed       bool
}

type memoryTransactionContext[T domain.IEntity[ID], ID comparable] struct {
	transaction *memoryTransaction[T, ID]
	owned       bool
}

type memoryTransactionKey[T domain.IEntity[ID], ID comparable] struct{}

// NewMemoryRepository 创建泛型内存仓储。
//
// generator 可为 nil；此时 Create 要求实体在调用前已具有非零 ID。实体 ID 为零且配置了 generator
// 时，实体还必须实现 domain.ISettableID，仓储才可回填生成值。
func NewMemoryRepository[T domain.IEntity[ID], ID comparable](
	generator gen.IGenerator[ID],
	opts ...MemoryOption[T, ID],
) *MemoryRepository[T, ID] {
	r := &MemoryRepository[T, ID]{
		state:     memoryState[T, ID]{entities: make(map[ID]T)},
		generator: generator,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	// 选项之间存在依赖（开闸阀依赖隔离声明），因此校验放在全部选项应用之后。
	r.validateCrossIsolationDeclaration()
	return r
}

// Create 创建实体；重复 ID 返回 errors.Conflict。
func (r *MemoryRepository[T, ID]) Create(ctx context.Context, entity T) error {
	if err := validateMemoryCall(ctx, entity); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	// 先定 ID 再校验约束，最后才落库——约束不通过时不得留下任何痕迹。
	if err := r.ensureID(entity); err != nil {
		return err
	}
	resource, constrained, err := r.requireConstraintFor(ctx, entity.GetID())
	if err != nil {
		return err
	}
	if isolated {
		if err := stampIsolation[T, ID](entity, isolationID); err != nil {
			return err
		}
	}
	if constrained {
		// 约束租户在隔离戳之后盖，两者冲突会被拦下（归属不可被写入篡改）。
		if err := r.stampConstraintTenant(entity, resource); err != nil {
			return err
		}
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		return r.createInState(state, entity)
	})
}

// Get 按 ID 返回实体的浅拷贝；不存在时返回 errors.NotFound。
func (r *MemoryRepository[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	if err := validateContext(ctx); err != nil {
		var zero T
		return zero, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	var result T
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		entity, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		if isolated && !visibleInIsolation[T, ID](entity, isolationID) {
			return entityNotFound(id)
		}
		result = shallowClone(entity)
		return nil
	})
	return result, err
}

// Update 覆盖已有实体；不存在时返回 errors.NotFound。
func (r *MemoryRepository[T, ID]) Update(ctx context.Context, entity T) error {
	if err := validateMemoryCall(ctx, entity); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	resource, constrained, err := r.requireConstraintFor(ctx, entity.GetID())
	if err != nil {
		return err
	}
	expectedVersion := uint64(0)
	if constrained {
		if expectedVersion, err = r.requireVersionedConstraint(resource); err != nil {
			return err
		}
		if err := r.checkEntityVersion(entity, expectedVersion); err != nil {
			return err
		}
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		id := entity.GetID()
		existing, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		if isolated {
			if !visibleInIsolation[T, ID](existing, isolationID) {
				return entityNotFound(id)
			}
			// 归属不可被更新篡改。
			if err := stampIsolation[T, ID](entity, isolationID); err != nil {
				return err
			}
		}
		if constrained {
			if err := r.checkStoredVersion(existing, expectedVersion); err != nil {
				return err
			}
			if err := r.stampConstraintTenant(entity, resource); err != nil {
				return err
			}
		}
		state.entities[id] = shallowClone(entity)
		return nil
	})
}

// Delete 物理删除实体；不存在时返回 errors.NotFound。
func (r *MemoryRepository[T, ID]) Delete(ctx context.Context, id ID) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	resource, constrained, err := r.requireConstraintFor(ctx, id)
	if err != nil {
		return err
	}
	expectedVersion := uint64(0)
	if constrained {
		if expectedVersion, err = r.requireVersionedConstraint(resource); err != nil {
			return err
		}
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		existing, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		if isolated && !visibleInIsolation[T, ID](existing, isolationID) {
			return entityNotFound(id)
		}
		if constrained {
			if err := r.checkStoredVersion(existing, expectedVersion); err != nil {
				return err
			}
		}
		delete(state.entities, id)
		state.order = removeID(state.order, id)
		return nil
	})
}

// Purge 物理删除实体，语义等同于 Delete。
func (r *MemoryRepository[T, ID]) Purge(ctx context.Context, id ID) error {
	return r.Delete(ctx, id)
}

// List 按创建顺序返回分页结果。
func (r *MemoryRepository[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return nil, err
	}
	var result []T
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		visible := make([]ID, 0, len(state.order))
		for _, id := range state.order {
			if isolated && !visibleInIsolation[T, ID](state.entities[id], isolationID) {
				continue
			}
			visible = append(visible, id)
		}
		start, end := pageBounds(len(visible), offset, limit)
		result = make([]T, 0, end-start)
		for _, id := range visible[start:end] {
			result = append(result, shallowClone(state.entities[id]))
		}
		return nil
	})
	return result, err
}

// Count 返回实体数量。
func (r *MemoryRepository[T, ID]) Count(ctx context.Context) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return 0, err
	}
	var count int64
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		if !isolated {
			count = int64(len(state.entities))
			return nil
		}
		for _, entity := range state.entities {
			if visibleInIsolation[T, ID](entity, isolationID) {
				count++
			}
		}
		return nil
	})
	return count, err
}

// Exists 判断实体是否存在。
func (r *MemoryRepository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	if err := validateContext(ctx); err != nil {
		return false, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return false, err
	}
	var exists bool
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		entity, ok := state.entities[id]
		exists = ok && (!isolated || visibleInIsolation[T, ID](entity, isolationID))
		return nil
	})
	return exists, err
}

// CreateAll 原子创建一批实体。
func (r *MemoryRepository[T, ID]) CreateAll(ctx context.Context, entities []T) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	for _, entity := range entities {
		if err := validateEntity(entity); err != nil {
			return err
		}
	}
	if len(entities) == 0 {
		return nil
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	resources := make([]scoped.ResourceConstraint, len(entities))
	constrained := false
	for i, entity := range entities {
		if err := r.ensureID(entity); err != nil {
			return err
		}
		resource, ok, err := r.requireConstraintFor(ctx, entity.GetID())
		if err != nil {
			return err
		}
		resources[i], constrained = resource, ok
	}
	if isolated {
		for _, entity := range entities {
			if err := stampIsolation[T, ID](entity, isolationID); err != nil {
				return err
			}
		}
	}
	if constrained {
		for i, entity := range entities {
			if err := r.stampConstraintTenant(entity, resources[i]); err != nil {
				return err
			}
		}
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		candidate := cloneMemoryState(*state)
		for _, entity := range entities {
			if err := r.createInState(&candidate, entity); err != nil {
				return err
			}
		}
		*state = candidate
		return nil
	})
}

// UpdateAll 原子更新一批实体。
func (r *MemoryRepository[T, ID]) UpdateAll(ctx context.Context, entities []T) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	for _, entity := range entities {
		if err := validateEntity(entity); err != nil {
			return err
		}
	}
	if len(entities) == 0 {
		return nil
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	checks := make(map[ID]memoryConstrainedWrite, len(entities))
	for _, entity := range entities {
		resource, constrained, err := r.requireConstraintFor(ctx, entity.GetID())
		if err != nil {
			return err
		}
		if !constrained {
			continue
		}
		expectedVersion, err := r.requireVersionedConstraint(resource)
		if err != nil {
			return err
		}
		if err := r.checkEntityVersion(entity, expectedVersion); err != nil {
			return err
		}
		checks[entity.GetID()] = memoryConstrainedWrite{resource: resource, expected: expectedVersion}
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		seen := make(map[ID]struct{}, len(entities))
		for _, entity := range entities {
			id := entity.GetID()
			if _, duplicate := seen[id]; duplicate {
				return duplicateEntityID(id)
			}
			seen[id] = struct{}{}
			existing, ok := state.entities[id]
			if !ok {
				return entityNotFound(id)
			}
			if isolated && !visibleInIsolation[T, ID](existing, isolationID) {
				return entityNotFound(id)
			}
			if check, ok := checks[id]; ok {
				if err := r.checkStoredVersion(existing, check.expected); err != nil {
					return err
				}
			}
		}
		for _, entity := range entities {
			if isolated {
				if err := stampIsolation[T, ID](entity, isolationID); err != nil {
					return err
				}
			}
			if check, ok := checks[entity.GetID()]; ok {
				if err := r.stampConstraintTenant(entity, check.resource); err != nil {
					return err
				}
			}
			state.entities[entity.GetID()] = shallowClone(entity)
		}
		return nil
	})
}

// DeleteAll 原子删除一批实体。
func (r *MemoryRepository[T, ID]) DeleteAll(ctx context.Context, ids []ID) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	expectedVersions := make(map[ID]uint64, len(ids))
	for _, id := range ids {
		resource, constrained, err := r.requireConstraintFor(ctx, id)
		if err != nil {
			return err
		}
		if !constrained {
			continue
		}
		expectedVersion, err := r.requireVersionedConstraint(resource)
		if err != nil {
			return err
		}
		expectedVersions[id] = expectedVersion
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		seen := make(map[ID]struct{}, len(ids))
		for _, id := range ids {
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			existing, ok := state.entities[id]
			if !ok {
				return entityNotFound(id)
			}
			if isolated && !visibleInIsolation[T, ID](existing, isolationID) {
				return entityNotFound(id)
			}
			if expectedVersion, ok := expectedVersions[id]; ok {
				if err := r.checkStoredVersion(existing, expectedVersion); err != nil {
					return err
				}
			}
		}
		for id := range seen {
			delete(state.entities, id)
		}
		state.order = filterIDs(state.order, seen)
		return nil
	})
}

// WithinTx 在内存快照事务中执行 fn；并发提交冲突返回 errors.Concurrency。
func (r *MemoryRepository[T, ID]) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return tx.RunTxLifecycle(ctx, r, fn)
}

// BeginTx 创建内存快照事务。
func (r *MemoryRepository[T, ID]) BeginTx(ctx context.Context) (tx.TxScope, error) {
	if err := validateContext(ctx); err != nil {
		return tx.TxScope{}, err
	}
	if transaction, owned, ok := r.transactionFromContext(ctx); ok {
		txCtx := context.WithValue(ctx, memoryTransactionKey[T, ID]{}, memoryTransactionContext[T, ID]{
			transaction: transaction,
			owned:       false,
		})
		return tx.NewTxScope(txCtx, false)
	} else if transaction != nil || owned {
		return tx.TxScope{}, errors.NewCode(errors.Internal, "invalid memory transaction context")
	}

	r.mu.RLock()
	transaction := &memoryTransaction[T, ID]{
		repository:   r,
		state:        cloneMemoryState(r.state),
		baseRevision: r.revision,
	}
	r.mu.RUnlock()
	txCtx := context.WithValue(ctx, memoryTransactionKey[T, ID]{}, memoryTransactionContext[T, ID]{
		transaction: transaction,
		owned:       true,
	})
	return tx.NewTxScope(txCtx, true)
}

// Commit 提交内存快照事务。
func (r *MemoryRepository[T, ID]) Commit(scope tx.TxScope) error {
	transaction, owned, ok := r.transactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "memory transaction not started")
	}
	if !owned || !scope.Owned() {
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
	return nil
}

// Rollback 丢弃内存快照事务。
func (r *MemoryRepository[T, ID]) Rollback(scope tx.TxScope) error {
	transaction, owned, ok := r.transactionFromContext(scope.Context())
	if !ok {
		return errors.NewCode(errors.InvalidInput, "memory transaction not started")
	}
	if !owned || !scope.Owned() {
		return nil
	}
	transaction.mu.Lock()
	transaction.closed = true
	transaction.mu.Unlock()
	return nil
}

// ensureID 在写入前补齐实体主键。
//
// 单独抽出的原因：受控约束按目标资源 ID 匹配，而 Create 的 ID 由仓储分配，
// 必须先定 ID 再校验约束，否则永远拿零值去匹配（与 SQL 侧 prepareCreate 同序）。
func (r *MemoryRepository[T, ID]) ensureID(entity T) error {
	var zero ID
	if entity.GetID() != zero {
		return nil
	}
	if r.generator == nil {
		return errors.NewCode(errors.InvalidInput, "entity ID is zero and no generator is configured")
	}
	settable, ok := any(entity).(domain.ISettableID[ID])
	if !ok {
		return errors.NewCode(errors.InvalidInput, "entity does not support generated ID assignment")
	}
	generated, err := r.generator.Next()
	if err != nil {
		return err
	}
	if generated == zero {
		return errors.NewCode(errors.FailedPrecondition, "ID generator returned zero value")
	}
	settable.SetID(generated)
	return nil
}

func (r *MemoryRepository[T, ID]) createInState(state *memoryState[T, ID], entity T) error {
	if err := r.ensureID(entity); err != nil {
		return err
	}
	id := entity.GetID()
	if _, exists := state.entities[id]; exists {
		return errors.NewCode(errors.Conflict, "entity already exists").WithContext("id", fmt.Sprint(id))
	}
	state.entities[id] = shallowClone(entity)
	state.order = append(state.order, id)
	return nil
}

func (r *MemoryRepository[T, ID]) withReadState(ctx context.Context, fn func(*memoryState[T, ID]) error) error {
	if transaction, _, ok := r.transactionFromContext(ctx); ok {
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return errors.NewCode(errors.FailedPrecondition, "memory transaction already closed")
		}
		return fn(&transaction.state)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return fn(&r.state)
}

func (r *MemoryRepository[T, ID]) withWriteState(ctx context.Context, fn func(*memoryState[T, ID]) error) error {
	if transaction, _, ok := r.transactionFromContext(ctx); ok {
		transaction.mu.Lock()
		defer transaction.mu.Unlock()
		if transaction.closed {
			return errors.NewCode(errors.FailedPrecondition, "memory transaction already closed")
		}
		return fn(&transaction.state)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := fn(&r.state); err != nil {
		return err
	}
	r.revision++
	return nil
}

func (r *MemoryRepository[T, ID]) transactionFromContext(ctx context.Context) (*memoryTransaction[T, ID], bool, bool) {
	if ctx == nil {
		return nil, false, false
	}
	state, ok := ctx.Value(memoryTransactionKey[T, ID]{}).(memoryTransactionContext[T, ID])
	if !ok || state.transaction == nil || state.transaction.repository != r {
		return nil, false, false
	}
	return state.transaction, state.owned, true
}

func cloneMemoryState[T domain.IEntity[ID], ID comparable](state memoryState[T, ID]) memoryState[T, ID] {
	cloned := memoryState[T, ID]{
		entities: make(map[ID]T, len(state.entities)),
		order:    append([]ID(nil), state.order...),
	}
	for id, entity := range state.entities {
		cloned.entities[id] = shallowClone(entity)
	}
	return cloned
}

func shallowClone[T any](value T) T {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Ptr || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return value
	}
	copyValue := reflect.New(rv.Elem().Type())
	copyValue.Elem().Set(rv.Elem())
	cloned, ok := copyValue.Interface().(T)
	if !ok {
		return value
	}
	return cloned
}

func validateMemoryCall[T any](ctx context.Context, entity T) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	return validateEntity(entity)
}

func validateEntity[T any](entity T) error {
	rv := reflect.ValueOf(entity)
	if !rv.IsValid() || ((rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface || rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Func) && rv.IsNil()) {
		return errors.NewCode(errors.InvalidInput, "entity is nil")
	}
	return nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

func entityNotFound[ID comparable](id ID) error {
	return errors.NewCode(errors.NotFound, "entity not found").WithContext("id", fmt.Sprint(id))
}

func duplicateEntityID[ID comparable](id ID) error {
	return errors.NewCode(errors.Conflict, "duplicate entity ID in batch").WithContext("id", fmt.Sprint(id))
}

func pageBounds(length, offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > length {
		offset = length
	}
	if limit <= 0 || limit > length-offset {
		limit = length - offset
	}
	return offset, offset + limit
}

func removeID[ID comparable](ids []ID, target ID) []ID {
	for i, id := range ids {
		if id == target {
			return append(ids[:i], ids[i+1:]...)
		}
	}
	return ids
}

func filterIDs[ID comparable](ids []ID, removed map[ID]struct{}) []ID {
	kept := ids[:0]
	for _, id := range ids {
		if _, ok := removed[id]; !ok {
			kept = append(kept, id)
		}
	}
	return kept
}

var (
	_ appcrud.ITransactional                                        = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
	_ domaincrud.IRepository[*domaincrud.Entity[int64], int64]      = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
	_ domaincrud.IQueryRepository[*domaincrud.Entity[int64], int64] = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
	_ domaincrud.IBatchOperations[*domaincrud.Entity[int64], int64] = (*MemoryRepository[*domaincrud.Entity[int64], int64])(nil)
)
