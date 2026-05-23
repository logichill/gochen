package contextx

import (
	stdctx "context"

	"gochen/errors"
)

// Ensure 确保 ctx 非 nil。
func Ensure(ctx stdctx.Context) (stdctx.Context, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx, nil
}
