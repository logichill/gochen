package testkit

import (
	"context"

	"gochen/errors"
)

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
