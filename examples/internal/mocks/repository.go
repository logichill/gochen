// Package mocks 提供示例中使用的模拟组件
package mocks

import (
	"context"
	"sync"
	"time"

	"gochen/domain/entity"
	"gochen/domain/service"
)

// GenericMockRepository 通用模拟仓储实现
type GenericMockRepository[T entity.IEntity[int64]] struct {
	entities map[int64]T
	nextID   int64
	mu       sync.RWMutex
	clock    func() time.Time // 用于测试的时间控制
}

// NewGenericMockRepository 创建通用模拟仓储
func NewGenericMockRepository[T entity.IEntity[int64]]() *GenericMockRepository[T] {
	return &GenericMockRepository[T]{
		entities: make(map[int64]T),
		nextID:   1,
		clock:    time.Now,
	}
}

// WithClock 设置时间函数（用于测试）
func (r *GenericMockRepository[T]) WithClock(clock func() time.Time) *GenericMockRepository[T] {
	r.clock = clock
	return r
}

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

func (r *GenericMockRepository[T]) GetByID(ctx context.Context, id int64) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entity, exists := r.entities[id]
	if !exists {
		var zero T
		return zero, service.NewNotFoundError("实体不存在: ID=%d", id)
	}
	return entity, nil
}

func (r *GenericMockRepository[T]) Update(ctx context.Context, entity T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entities[entity.GetID()]; !exists {
		return service.NewNotFoundError("实体不存在: ID=%d", entity.GetID())
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

func (r *GenericMockRepository[T]) Delete(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entities[id]; !exists {
		return service.NewNotFoundError("实体不存在: ID=%d", id)
	}
	delete(r.entities, id)
	return nil
}

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

func (r *GenericMockRepository[T]) Count(ctx context.Context) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return int64(len(r.entities)), nil
}

func (r *GenericMockRepository[T]) Exists(ctx context.Context, id int64) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.entities[id]
	return exists, nil
}

// CreateBatch 批量创建
func (r *GenericMockRepository[T]) CreateBatch(ctx context.Context, entities []T) error {
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

// UpdateBatch 批量更新
func (r *GenericMockRepository[T]) UpdateBatch(ctx context.Context, entities []T) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock()
	for i := range entities {
		if _, exists := r.entities[entities[i].GetID()]; !exists {
			return service.NewNotFoundError("实体不存在: ID=%d", entities[i].GetID())
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

// DeleteBatch 批量删除
func (r *GenericMockRepository[T]) DeleteBatch(ctx context.Context, ids []int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		if _, exists := r.entities[id]; !exists {
			return service.NewNotFoundError("实体不存在: ID=%d", id)
		}
		delete(r.entities, id)
	}
	return nil
}
