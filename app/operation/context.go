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
