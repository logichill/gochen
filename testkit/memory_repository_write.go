package testkit

import (
	"context"
	"fmt"

	"gochen/domain"
	"gochen/errors"
)

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
