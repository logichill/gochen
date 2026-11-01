package testkit

import (
	"sync"

	"gochen/domain/audited"
	domaincrud "gochen/domain/crud"
	"gochen/gen"
)

// AuditedMemoryRepository 在 MemoryRepository 之上提供软删除查询与事务化审计记录。
//
// 实体变更和审计记录在同一个乐观内存事务中提交；事务期间若仓储被其他调用修改，提交会返回
// errors.Concurrency。该实现适合 audited application 的示例和单元测试。
type AuditedMemoryRepository[T audited.IAuditedEntity[ID], ID comparable] struct {
	*MemoryRepository[T, ID]

	auditMu       sync.RWMutex
	audits        map[string][]audited.AuditRecord
	nextAuditID   int64
	auditRevision uint64
}

// NewAuditedMemoryRepository 创建审计型内存仓储。
func NewAuditedMemoryRepository[T audited.IAuditedEntity[ID], ID comparable](generator gen.IGenerator[ID]) *AuditedMemoryRepository[T, ID] {
	return &AuditedMemoryRepository[T, ID]{
		MemoryRepository: NewMemoryRepository[T, ID](generator),
		audits:           make(map[string][]audited.AuditRecord),
		nextAuditID:      1,
	}
}

var (
	_ domaincrud.IRepository[audited.IAuditedEntity[int64], int64]          = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ domaincrud.IPurgeRepository[audited.IAuditedEntity[int64], int64]     = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IRestoreRepository[audited.IAuditedEntity[int64], int64]      = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IDeletedQueryRepository[audited.IAuditedEntity[int64], int64] = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IAuditStore                                                   = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IAuditQueryStore                                              = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
	_ audited.IBulkAuditStore                                               = (*AuditedMemoryRepository[audited.IAuditedEntity[int64], int64])(nil)
)
