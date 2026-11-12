package testkit

import (
	domain "gochen/domain"
	domcrud "gochen/domain/crud"
	"sync"

	audited "gochen/audit"

	"gochen/gen"
)

// AuditedMemoryRepository 在 MemoryRepository 之上提供事务化审计记录，不要求实体包含删除或归因字段。
//
// 实体变更和审计记录在同一个乐观内存事务中提交；事务期间若仓储被其他调用修改，提交会返回
// errors.Concurrency。该实现适合独立审计写入及其与删除能力组合的示例和单元测试。
type AuditedMemoryRepository[T domain.IEntity[ID], ID comparable] struct {
	*MemoryRepository[T, ID]

	auditMu       sync.RWMutex
	audits        map[string][]memoryAuditRecord
	nextAuditID   int64
	auditRevision uint64
}

// NewAuditedMemoryRepository 创建支持事务化审计记录的内存仓储。
func NewAuditedMemoryRepository[T domain.IEntity[ID], ID comparable](generator gen.IGenerator[ID], opts ...MemoryOption[T, ID]) (*AuditedMemoryRepository[T, ID], error) {
	repository, err := NewMemoryRepository[T, ID](generator, opts...)
	if err != nil {
		return nil, err
	}
	return &AuditedMemoryRepository[T, ID]{
		MemoryRepository: repository,
		audits:           make(map[string][]memoryAuditRecord),
		nextAuditID:      1,
	}, nil
}

var (
	_ domcrud.IRepository[domain.IEntity[int64], int64]             = (*AuditedMemoryRepository[domain.IEntity[int64], int64])(nil)
	_ domcrud.IPurgeRepository[domain.IEntity[int64], int64]        = (*AuditedMemoryRepository[domain.IEntity[int64], int64])(nil)
	_ domcrud.IRestoreRepository[domain.IEntity[int64], int64]      = (*AuditedMemoryRepository[domain.IEntity[int64], int64])(nil)
	_ domcrud.IDeletedQueryRepository[domain.IEntity[int64], int64] = (*AuditedMemoryRepository[domain.IEntity[int64], int64])(nil)
	_ audited.IAuditStore                                           = (*AuditedMemoryRepository[domain.IEntity[int64], int64])(nil)
	_ audited.IAuditQueryStore                                      = (*AuditedMemoryRepository[domain.IEntity[int64], int64])(nil)
	_ audited.IBulkAuditStore                                       = (*AuditedMemoryRepository[domain.IEntity[int64], int64])(nil)
)
