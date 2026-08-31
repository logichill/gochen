package contextx

import (
	"context"

	"gochen/errors"
)

// Ensure 确保 ctx 非 nil。
func Ensure(ctx context.Context) (context.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx, nil
}
