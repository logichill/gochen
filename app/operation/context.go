package operation

import (
	"context"
	"sync"

	"gochen/errors"
)

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}

type contextKey string

const operationIDContextKey contextKey = "gochen/app/operation/id"

type operationContext struct {
	id        string
	mu        sync.Mutex
	transfers map[[2]string]bool
}

// WithOperationID 将 operation id 注入上下文，供业务侧读取。
func WithOperationID(ctx context.Context, operationID string) context.Context {
	if ctx == nil || operationID == "" {
		return ctx
	}
	if state, ok := ctx.Value(operationIDContextKey).(*operationContext); ok && state.id == operationID {
		return ctx
	}
	return context.WithValue(ctx, operationIDContextKey, &operationContext{id: operationID, transfers: map[[2]string]bool{}})
}

// OperationIDFromContext 读取当前上下文中的 operation id。
func OperationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	state, _ := ctx.Value(operationIDContextKey).(*operationContext)
	if state == nil {
		return ""
	}
	return state.id
}

// EnsureOperationID 在事务开始前复用或生成一次逻辑操作编号。
func EnsureOperationID(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	if OperationIDFromContext(ctx) != "" {
		return ctx
	}
	return WithOperationID(ctx, defaultOperationID())
}

// ReserveTransfer 防止同一操作批次重复迁移同一资源实体；失败时释放预留。
func ReserveTransfer(ctx context.Context, resource, entity string) (func(), error) {
	state, _ := ctx.Value(operationIDContextKey).(*operationContext)
	if state == nil {
		return nil, errors.NewCode(errors.Internal, "operation id must precede transfer reservation")
	}
	key := [2]string{resource, entity}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.transfers[key] {
		return nil, errors.NewCode(errors.InvalidInput, "entity already transferred in this operation")
	}
	state.transfers[key] = true
	return func() { state.mu.Lock(); delete(state.transfers, key); state.mu.Unlock() }, nil
}
