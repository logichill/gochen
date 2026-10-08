package audited

import (
	"context"
	"gochen/domain/audited"
)

// IAuditOperations 定义独立于基础 Delete 的审计应用扩展。
// 安全装饰器和传输适配器共享此方法集；存在该能力才暴露审计操作。
type IAuditOperations[T any, ID comparable] interface {
	// HasPurgeCapability 报告是否具备硬删除能力，供传输层选择暴露的路由；不代表调用方已获授权。
	HasPurgeCapability() bool
	AuditStore() audited.IAuditStore
	AuditTrail(ctx context.Context, id ID, offset, limit int) ([]audited.AuditRecord, error)
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
	Restore(ctx context.Context, id ID, by string) error
	Purge(ctx context.Context, id ID) error
}
