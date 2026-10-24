package audited

import (
	"context"

	appcrud "gochen/app/crud"
	"gochen/domain"
	domaudited "gochen/domain/audited"
)

// IApplication 定义 CRUD 及审计扩展用例。
type IApplication[T domain.IEntity[ID], ID comparable] interface {
	appcrud.IApplication[T, ID]

	IAuditOperations[T, ID]
}

// IAuditOperations 定义独立于基础 Delete 的审计应用扩展。
// 安全装饰器和传输适配器共享此方法集；存在该能力才暴露审计操作。
type IAuditOperations[T any, ID comparable] interface {
	// HasPurgeCapability 报告是否具备硬删除能力，供传输层选择暴露的路由；不代表调用方已获授权。
	HasPurgeCapability() bool
	// HasAuditStore 报告底层审计存储是否已就绪；不对外暴露存储句柄，
	// 避免调用方绕过安全控制直接改写审计数据。
	HasAuditStore() bool
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]domaudited.AuditRecord, error)
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	// GetWithDeleted 按主键读取实体（含已软删记录），安全装饰器必须实现为受控读。
	GetWithDeleted(ctx context.Context, id ID) (T, error)
	Restore(ctx context.Context, id ID, by string) error
	Purge(ctx context.Context, id ID) error
}
