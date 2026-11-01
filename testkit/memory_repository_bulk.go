package testkit

import (
	"context"
	"gochen/domain/spec"

	"gochen/domain"
)

// CreateBulk 原子创建一批实体；规约对每个实体分别做 WITH CHECK。
func (r *MemoryRepository[T, ID]) CreateBulk(ctx context.Context, entities []T, s spec.Spec) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := requireWriteSpec(s, true); err != nil {
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
	for _, entity := range entities {
		if err := r.ensureID(entity); err != nil {
			return err
		}
	}
	if isolated {
		for _, entity := range entities {
			if err := stampIsolation[T, ID](entity, isolationID); err != nil {
				return err
			}
		}
	}
	for _, entity := range entities {
		if err := r.checkWriteSpec(s, entity); err != nil {
			return err
		}
	}
	return r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
		candidate := cloneMemoryState(*state)
		for _, entity := range entities {
			if err := r.createInState(&candidate, shallowClone(entity)); err != nil {
				return err
			}
		}
		*state = candidate
		return nil
	})
}

// UpdateBulk 原子更新一批实体；规约同时约束读取边界与写入结果。
func (r *MemoryRepository[T, ID]) UpdateBulk(ctx context.Context, entities []T, s spec.Spec) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := requireWriteSpec(s, false); err != nil {
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
	expectedVersions := make(map[ID]uint64, len(entities))
	for _, entity := range entities {
		expectedVersions[entity.GetID()] = entity.GetVersion()
	}
	err = r.withWriteState(ctx, func(state *memoryState[T, ID]) error {
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
			if versioned[T, ID](entity) && existing.GetVersion() != expectedVersions[id] {
				return concurrencyError(id, expectedVersions[id])
			}
		}
		candidate := cloneMemoryState(*state)
		for _, entity := range entities {
			id := entity.GetID()
			if isolated {
				if err := stampIsolation[T, ID](entity, isolationID); err != nil {
					return err
				}
			}
			if err := r.checkWriteSpec(s, entity); err != nil {
				return err
			}
			if setter, ok := any(entity).(domain.ISettableVersion); ok {
				setter.SetVersion(expectedVersions[id] + 1)
			}
			candidate.entities[id] = shallowClone(entity)
		}
		*state = candidate
		return nil
	})
	if err != nil {
		for _, entity := range entities {
			if setter, ok := any(entity).(domain.ISettableVersion); ok {
				setter.SetVersion(expectedVersions[entity.GetID()])
			}
		}
	}
	return err
}

// DeleteBulk 原子物理删除一批实体；不存在、已软删或被规约过滤时返回 errors.NotFound。
func (r *MemoryRepository[T, ID]) DeleteBulk(ctx context.Context, entities []T, s spec.Spec) error {
	if err := validateContext(ctx); err != nil {
		return err
	}
	if err := requireWriteSpec(s, false); err != nil {
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
	expectedVersions := make(map[ID]uint64, len(entities))
	for _, entity := range entities {
		expectedVersions[entity.GetID()] = entity.GetVersion()
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
			if versioned[T, ID](entity) && existing.GetVersion() != expectedVersions[id] {
				return concurrencyError(id, expectedVersions[id])
			}
		}
		for id := range seen {
			delete(state.entities, id)
		}
		state.order = filterIDs(state.order, seen)
		return nil
	})
}
