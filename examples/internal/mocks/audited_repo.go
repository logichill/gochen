// Package mocks 提供审计型内存仓储（示例/测试用）
package mocks

import (
	"context"
	"fmt"
	"time"

	"gochen/domain/entity"
	srepo "gochen/domain/repository"
)

// MockAuditedRepository 基于内存的审计型仓储实现
// - 复用 GenericMockRepository 提供的基础 CRUD 行为
// - 实现审计相关方法（审计轨迹、软删/恢复/物理删）
type MockAuditedRepository[T entity.Entity[int64]] struct {
	*GenericMockRepository[T]
	audits map[int64][]srepo.AuditRecord
}

// NewMockAuditedRepository 创建审计型内存仓储
func NewMockAuditedRepository[T entity.Entity[int64]]() *MockAuditedRepository[T] {
	return &MockAuditedRepository[T]{
		GenericMockRepository: NewGenericMockRepository[T](),
		audits:                map[int64][]srepo.AuditRecord{},
	}
}

func (r *MockAuditedRepository[T]) record(id int64, op, by string, changes map[string]any) {
	rec := srepo.AuditRecord{
		ID:        time.Now().UnixNano(),
		EntityID:  fmt.Sprintf("%d", id),
		Operation: op,
		Operator:  by,
		Timestamp: time.Now(),
		Changes:   changes,
	}
	r.audits[id] = append(r.audits[id], rec)
}

// GetAuditTrail 获取审计轨迹
func (r *MockAuditedRepository[T]) GetAuditTrail(ctx context.Context, id int64) ([]srepo.AuditRecord, error) {
	return r.audits[id], nil
}

// ListDeleted 查询已软删除实体
func (r *MockAuditedRepository[T]) ListDeleted(ctx context.Context, offset, limit int) ([]T, error) {
	all, _ := r.List(ctx, 0, 1<<30)
	res := make([]T, 0)
	for _, v := range all {
		if v.IsDeleted() {
			res = append(res, v)
		}
	}
	// 简化分页（示例用）
	if offset >= len(res) {
		return []T{}, nil
	}
	end := offset + limit
	if end > len(res) {
		end = len(res)
	}
	return res[offset:end], nil
}

// Restore 恢复已软删除实体
func (r *MockAuditedRepository[T]) Restore(ctx context.Context, id int64, by string) error {
	e, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := e.Restore(); err != nil {
		return err
	}
	e.SetUpdatedInfo(by, time.Now())
	r.record(id, "RESTORE", by, nil)
	return r.Update(ctx, e)
}

// SoftDelete 执行软删除
func (r *MockAuditedRepository[T]) SoftDelete(ctx context.Context, id int64, by string) error {
	e, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := e.SoftDelete(by, time.Now()); err != nil {
		return err
	}
	r.record(id, "DELETE", by, nil)
	return r.Update(ctx, e)
}

// PermanentDelete 物理删除
func (r *MockAuditedRepository[T]) PermanentDelete(ctx context.Context, id int64) error {
	r.record(id, "DELETE_HARD", "admin", nil)
	return r.Delete(ctx, id)
}
