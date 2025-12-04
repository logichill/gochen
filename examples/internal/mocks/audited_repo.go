// Package mocks 提供审计型内存仓储（示例/测试用）
package mocks

import (
	"context"
	"fmt"
	"strconv"

	"gochen/domain/audited"
)

// MockAuditedRepository 基于内存的审计型仓储实现。
// - 复用 GenericMockRepository 提供的基础 CRUD 行为；
// - 实现 audited.IAuditStore，用于保存与查询审计记录。
type MockAuditedRepository[T audited.IAuditedEntity[int64]] struct {
	*GenericMockRepository[T]
	audits map[int64][]audited.AuditRecord
}

// NewMockAuditedRepository 创建审计型内存仓储
func NewMockAuditedRepository[T audited.IAuditedEntity[int64]]() *MockAuditedRepository[T] {
	return &MockAuditedRepository[T]{
		GenericMockRepository: NewGenericMockRepository[T](),
		audits:                map[int64][]audited.AuditRecord{},
	}
}

// SaveAuditRecord 保存审计记录，实现 audited.IAuditStore。
func (r *MockAuditedRepository[T]) SaveAuditRecord(ctx context.Context, rec audited.AuditRecord) error {
	id, err := strconv.ParseInt(rec.EntityID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid entity id for audit record: %s", rec.EntityID)
	}
	r.audits[id] = append(r.audits[id], rec)
	return nil
}

// ListAuditRecordsByEntity 按实体 ID 查询审计记录，实现 audited.IAuditStore。
func (r *MockAuditedRepository[T]) ListAuditRecordsByEntity(ctx context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	id, err := strconv.ParseInt(entityID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid entity id for audit query: %s", entityID)
	}
	recs := r.audits[id]
	if offset >= len(recs) {
		return []audited.AuditRecord{}, nil
	}
	end := offset + limit
	if end > len(recs) {
		end = len(recs)
	}
	return recs[offset:end], nil
}
