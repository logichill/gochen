package testkit

import (
	"context"

	"gochen/auth/scoped"
)

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
			if err := r.createInState(&candidate, shallowClone(entity)); err != nil {
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
		candidate := cloneMemoryState(*state)
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
			candidate.entities[entity.GetID()] = shallowClone(entity)
		}
		*state = candidate
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
