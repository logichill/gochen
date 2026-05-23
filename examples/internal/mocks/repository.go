// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gochen/domain"
	"gochen/errors"
)

// GenericMockRepository 是示例中最基础的内存仓储桩实现。
type GenericMockRepository[T domain.IEntity[int64]] struct {
	entities map[int64]T
	nextID   int64
	mu       sync.RWMutex
	clock    func() time.Time // 用于测试的时间控制
}

// NewGenericMockRepository 创建一个带自增 ID 的内存仓储。
func NewGenericMockRepository[T domain.IEntity[int64]]() *GenericMockRepository[T] {
	return &GenericMockRepository[T]{
		entities: make(map[int64]T),
		nextID:   1,
		clock:    time.Now,
	}
}

// WithClock 覆盖仓储内部使用的时间函数。
func (r *GenericMockRepository[T]) WithClock(clock func() time.Time) *GenericMockRepository[T] {
	r.clock = clock
	return r
}

// Create 把实体写入内存仓储，并尽力补齐 ID 与更新时间。
func (r *GenericMockRepository[T]) Create(ctx context.Context, entity T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 设置 ID（使用类型断言检查是否有 SetID 方法）
	if withID, ok := any(entity).(interface {
		SetID(id int64)
	}); ok {
		withID.SetID(r.nextID)
	}
	r.nextID++

	// 设置时间戳
	now := r.clock()
	if withTimestamps, ok := any(entity).(interface {
		SetUpdatedAt(t int64)
	}); ok {
		withTimestamps.SetUpdatedAt(now.Unix())
	}

	r.entities[entity.GetID()] = entity
	return nil
}

// Get 按 ID 读取一条实体记录。
func (r *GenericMockRepository[T]) Get(ctx context.Context, id int64) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entity, exists := r.entities[id]
	if !exists {
		var zero T
		return zero, errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", id))
	}
	return entity, nil
}

// Update 按实体 ID 覆盖已有记录，并刷新更新时间。
func (r *GenericMockRepository[T]) Update(ctx context.Context, entity T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entities[entity.GetID()]; !exists {
		return errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", entity.GetID()))
	}

	// 更新时间戳
	now := r.clock()
	if withTimestamps, ok := any(entity).(interface {
		SetUpdatedAt(t int64)
	}); ok {
		withTimestamps.SetUpdatedAt(now.Unix())
	}

	r.entities[entity.GetID()] = entity
	return nil
}

// Delete 按 ID 删除一条实体记录。
func (r *GenericMockRepository[T]) Delete(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entities[id]; !exists {
		return errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", id))
	}
	delete(r.entities, id)
	return nil
}

// List 返回一个简单的偏移/限制切片视图。
func (r *GenericMockRepository[T]) List(ctx context.Context, offset, limit int) ([]T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entities := make([]T, 0, len(r.entities))
	for _, entity := range r.entities {
		entities = append(entities, entity)
	}

	if offset >= len(entities) {
		return []T{}, nil
	}

	end := offset + limit
	if end > len(entities) {
		end = len(entities)
	}

	return entities[offset:end], nil
}

// Count 返回当前仓储中的实体数量。
func (r *GenericMockRepository[T]) Count(ctx context.Context) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return int64(len(r.entities)), nil
}

// Exists 判断指定 ID 的实体是否存在。
func (r *GenericMockRepository[T]) Exists(ctx context.Context, id int64) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.entities[id]
	return exists, nil
}

// CreateAll 批量创建实体，并为每条记录补齐 ID/更新时间。
func (r *GenericMockRepository[T]) CreateAll(ctx context.Context, entities []T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock()
	for i := range entities {
		// 设置 ID（使用类型断言检查是否有 SetID 方法）
		if withID, ok := any(entities[i]).(interface {
			SetID(id int64)
		}); ok {
			withID.SetID(r.nextID)
		}
		r.nextID++

		// 更新时间戳
		if withTimestamps, ok := any(entities[i]).(interface {
			SetUpdatedAt(t int64)
		}); ok {
			withTimestamps.SetUpdatedAt(now.Unix())
		}

		r.entities[entities[i].GetID()] = entities[i]
	}
	return nil
}

// UpdateAll 批量更新已有实体。
func (r *GenericMockRepository[T]) UpdateAll(ctx context.Context, entities []T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock()
	for i := range entities {
		if _, exists := r.entities[entities[i].GetID()]; !exists {
			return errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", entities[i].GetID()))
		}
		// 更新时间戳
		if withTimestamps, ok := any(entities[i]).(interface {
			SetUpdatedAt(t int64)
		}); ok {
			withTimestamps.SetUpdatedAt(now.Unix())
		}

		r.entities[entities[i].GetID()] = entities[i]
	}
	return nil
}

// DeleteAll 批量删除给定 ID 对应的实体。
func (r *GenericMockRepository[T]) DeleteAll(ctx context.Context, ids []int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		if _, exists := r.entities[id]; !exists {
			return errors.NewCode(errors.NotFound, fmt.Sprintf("实体不存在: ID=%d", id))
		}
		delete(r.entities, id)
	}
	return nil
}
