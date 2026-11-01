package testkit

import (
	"context"

	"gochen/domain/spec"
)

// Get 按 ID 返回实体的浅拷贝；不存在、已软删或被规约过滤时返回 errors.NotFound。
func (r *MemoryRepository[T, ID]) Get(ctx context.Context, id ID, s spec.Spec) (T, error) {
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
		if isolated && !visibleInIsolation[T, ID](entity, isolationID) {
			return entityNotFound(id)
		}
		ok, matchErr := r.visibleEntity(s, entity)
		if matchErr != nil {
			return matchErr
		}
		if !ok {
			return entityNotFound(id)
		}
		result = shallowClone(entity)
		return nil
	})
	return result, err
}

// List 按创建顺序返回满足规约的分页结果。
func (r *MemoryRepository[T, ID]) List(ctx context.Context, offset, limit int, s spec.Spec) ([]T, error) {
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
			if isolated && !visibleInIsolation[T, ID](entity, isolationID) {
				continue
			}
			ok, matchErr := r.visibleEntity(s, entity)
			if matchErr != nil {
				return matchErr
			}
			if !ok {
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

// Count 返回满足规约的实体数量。
func (r *MemoryRepository[T, ID]) Count(ctx context.Context, s spec.Spec) (int64, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	isolationID, isolated, err := r.readIsolationID(ctx)
	if err != nil {
		return 0, err
	}
	var count int64
	err = r.withReadState(ctx, func(state *memoryState[T, ID]) error {
		for _, entity := range state.entities {
			if isolated && !visibleInIsolation[T, ID](entity, isolationID) {
				continue
			}
			ok, matchErr := r.visibleEntity(s, entity)
			if matchErr != nil {
				return matchErr
			}
			if ok {
				count++
			}
		}
		return nil
	})
	return count, err
}

// Exists 判断实体是否存在且满足规约。
func (r *MemoryRepository[T, ID]) Exists(ctx context.Context, id ID, s spec.Spec) (bool, error) {
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
		if !ok {
			return nil
		}
		if isolated && !visibleInIsolation[T, ID](entity, isolationID) {
			return nil
		}
		ok, matchErr := r.visibleEntity(s, entity)
		if matchErr != nil {
			return matchErr
		}
		exists = ok
		return nil
	})
	return exists, err
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
