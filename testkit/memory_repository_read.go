package testkit

import (
	"context"
)

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
