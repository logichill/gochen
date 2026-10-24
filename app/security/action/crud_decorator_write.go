package action

import (
	"context"

	authaction "gochen/auth/action"
)

// --- IWriter ---

func (a *application[T, ID]) Create(ctx context.Context, e T) error {
	if err := a.require(ctx, authaction.OpCreate); err != nil {
		return err
	}
	return a.inner.Create(ctx, e)
}

func (a *application[T, ID]) Update(ctx context.Context, e T) error {
	if err := a.require(ctx, authaction.OpUpdate); err != nil {
		return err
	}
	return a.inner.Update(ctx, e)
}

func (a *application[T, ID]) Delete(ctx context.Context, id ID) error {
	if err := a.require(ctx, authaction.OpDelete); err != nil {
		return err
	}
	return a.inner.Delete(ctx, id)
}
