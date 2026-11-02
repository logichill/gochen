package testkit

import (
	"context"
	"fmt"
	"gochen/domain/spec"

	"gochen/domain"
	"gochen/errors"
)

// Create 创建实体；写入前以规约做 WITH CHECK，重复 ID 返回 errors.Conflict。
func (r *MemoryRepository[T, ID]) Create(ctx context.Context, entity T, s spec.Spec) error {
	if err := validateMemoryCall(ctx, entity); err != nil {
		return err
	}
	if err := r.execution.ValidateWriteSpec(s, true); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	// 先定 ID 再校验规约，最后才落库——规约不通过时不得留下任何痕迹。
	if err := r.ensureID(entity); err != nil {
		return err
	}
	if isolated {
		if err := r.stampIsolation(entity, isolationID); err != nil {
			return err
		}
	}
	if err := r.checkWriteSpec(s, entity); err != nil {
		return err
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		return r.createInState(state, entity)
	})
}

// Update 覆盖已有实体；不存在、已软删或被规约过滤时返回 errors.NotFound，
// 版本不符返回 errors.Concurrency，写入后不满足规约返回 errors.Forbidden。
func (r *MemoryRepository[T, ID]) Update(ctx context.Context, entity T, s spec.Spec) error {
	if err := validateMemoryCall(ctx, entity); err != nil {
		return err
	}
	if err := r.execution.ValidateWriteSpec(s, false); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	expectedVersion := entity.GetVersion()
	err = r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		id := entity.GetID()
		existing, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		if isolated {
			if !r.visibleInIsolation(existing, isolationID) {
				return entityNotFound(id)
			}
		}
		if softDeleted(existing) {
			return entityNotFound(id)
		}
		visible, matchErr := r.matchSpec(s, existing)
		if matchErr != nil {
			return matchErr
		}
		if !visible {
			return entityNotFound(id)
		}
		if versioned[T, ID](entity) && existing.GetVersion() != expectedVersion {
			return concurrencyError(id, expectedVersion)
		}
		protected, err := r.execution.CaptureProtected(existing)
		if err != nil {
			return err
		}
		if err := r.execution.CheckProtected(protected, entity); err != nil {
			return err
		}
		if err := r.checkWriteSpec(s, entity); err != nil {
			return err
		}
		if setter, ok := any(entity).(domain.ISettableVersion); ok {
			setter.SetVersion(expectedVersion + 1)
		}
		state.entities[id] = cloneEntityState(entity)
		return nil
	})
	if err != nil {
		if setter, ok := any(entity).(domain.ISettableVersion); ok {
			setter.SetVersion(expectedVersion)
		}
	}
	return err
}

// Delete 物理删除实体；不存在、已软删或被规约过滤时返回 errors.NotFound。
func (r *MemoryRepository[T, ID]) Delete(ctx context.Context, entity T, s spec.Spec) error {
	return r.delete(ctx, entity, s, false)
}

// Purge 物理删除实体，包括已软删的行。
func (r *MemoryRepository[T, ID]) Purge(ctx context.Context, entity T, s spec.Spec) error {
	return r.delete(ctx, entity, s, true)
}

func (r *MemoryRepository[T, ID]) delete(ctx context.Context, entity T, s spec.Spec, includeDeleted bool) error {
	if err := validateMemoryCall(ctx, entity); err != nil {
		return err
	}
	if err := r.execution.ValidateWriteSpec(s, false); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	id := entity.GetID()
	expectedVersion := entity.GetVersion()
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		existing, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		if isolated && !r.visibleInIsolation(existing, isolationID) {
			return entityNotFound(id)
		}
		if !includeDeleted && softDeleted(existing) {
			return entityNotFound(id)
		}
		visible, matchErr := r.matchSpec(s, existing)
		if matchErr != nil {
			return matchErr
		}
		if !visible {
			return entityNotFound(id)
		}
		if versioned[T, ID](entity) && existing.GetVersion() != expectedVersion {
			return concurrencyError(id, expectedVersion)
		}
		delete(state.entities, id)
		state.order = removeID(state.order, id)
		return nil
	})
}

// GetWithDeleted 读取实体（包含已软删记录），按规约过滤。
func (r *MemoryRepository[T, ID]) GetWithDeleted(ctx context.Context, id ID, s spec.Spec) (T, error) {
	var zero T
	if err := validateContext(ctx); err != nil {
		return zero, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return zero, err
	}
	var result T
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		entity, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		if isolated && !r.visibleInIsolation(entity, isolationID) {
			return entityNotFound(id)
		}
		visible, matchErr := r.matchSpec(s, entity)
		if matchErr != nil {
			return matchErr
		}
		if !visible {
			return entityNotFound(id)
		}
		result = cloneEntityState(entity)
		return nil
	})
	return result, err
}

// Restore 恢复已软删实体；目标可见但未处于删除态时返回 errors.Conflict，
// 恢复后的状态不满足规约时返回 errors.Forbidden。
func (r *MemoryRepository[T, ID]) Restore(ctx context.Context, entity T, s spec.Spec) error {
	if err := validateMemoryCall(ctx, entity); err != nil {
		return err
	}
	if err := r.execution.ValidateWriteSpec(s, false); err != nil {
		return err
	}
	if err := r.guardCrossIsolationWrite(ctx); err != nil {
		return err
	}
	isolationID, isolated, err := r.isolationID(ctx)
	if err != nil {
		return err
	}
	id := entity.GetID()
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		existing, ok := state.entities[id]
		if !ok {
			return entityNotFound(id)
		}
		if isolated && !r.visibleInIsolation(existing, isolationID) {
			return entityNotFound(id)
		}
		visible, matchErr := r.matchSpec(s, existing)
		if matchErr != nil {
			return matchErr
		}
		if !visible {
			return entityNotFound(id)
		}
		if !softDeleted(existing) {
			return errors.NewCode(errors.Conflict, "entity is not deleted").
				WithContext("id", fmt.Sprint(id))
		}
		if existing.GetVersion() != entity.GetVersion() {
			return concurrencyError(id, entity.GetVersion())
		}
		protected, err := r.execution.CaptureProtected(existing)
		if err != nil {
			return err
		}
		if err := r.execution.CheckProtected(protected, entity); err != nil {
			return err
		}
		if err := r.checkWriteSpec(s, entity); err != nil {
			return err
		}
		if setter, ok := any(entity).(domain.ISettableVersion); ok {
			setter.SetVersion(entity.GetVersion() + 1)
		}
		state.entities[id] = cloneEntityState(entity)
		return nil
	})
}

// ListDeleted 返回满足规约的已软删实体列表。
func (r *MemoryRepository[T, ID]) ListDeleted(ctx context.Context, offset, limit int, s spec.Spec) ([]T, error) {
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
			entity := state.entities[id]
			if isolated && !r.visibleInIsolation(entity, isolationID) {
				continue
			}
			if !softDeleted(entity) {
				continue
			}
			ok, matchErr := r.matchSpec(s, entity)
			if matchErr != nil {
				return matchErr
			}
			if ok {
				visible = append(visible, id)
			}
		}
		start, end := pageBounds(len(visible), offset, limit)
		result = make([]T, 0, end-start)
		for _, id := range visible[start:end] {
			result = append(result, cloneEntityState(state.entities[id]))
		}
		return nil
	})
	return result, err
}

func (r *MemoryRepository[T, ID]) checkWriteSpec(s spec.Spec, entity T) error {
	ok, err := r.matchSpec(s, entity)
	if err != nil {
		return err
	}
	if !ok {
		return errors.NewCode(errors.Forbidden, "entity does not satisfy the required scope").
			WithContext("id", fmt.Sprint(entity.GetID())).
			WithContext("spec", s.Canonical())
	}
	return nil
}

func versioned[T domain.IEntity[ID], ID comparable](entity T) bool {
	_, ok := any(entity).(domain.ISettableVersion)
	return ok
}

func concurrencyError[ID comparable](id ID, expected uint64) error {
	return errors.NewCode(errors.Concurrency, "concurrent modification detected").
		WithContext("id", fmt.Sprint(id)).
		WithContext("expected_version", expected)
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
	state.entities[id] = cloneEntityState(entity)
	state.order = append(state.order, id)
	return nil
}
