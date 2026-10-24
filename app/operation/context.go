package operation

import (
	"context"

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

// WithOperationID 将 operation id 注入上下文，供业务侧读取。
func WithOperationID(ctx context.Context, operationID string) context.Context {
	if ctx == nil || operationID == "" {
		return ctx
	}
	return context.WithValue(ctx, operationIDContextKey, operationID)
}

// OperationIDFromContext 读取当前上下文中的 operation id。
func OperationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(operationIDContextKey).(string)
	return value
}
