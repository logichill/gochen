package audited

import (
	"context"
	"gochen/auth"
)

// Transfer 原子迁移归属并写入共享操作编号的两侧审计。
func (a *Application[T, ID]) Transfer(ctx context.Context, action string, id ID, expectedVersion uint64, targets []auth.Assignment, opts ...ExecOption) (T, error) {
	return a.core.Transfer(ctx, action, id, expectedVersion, targets, opts...)
}
