package action

import (
	"context"

	"gochen/app/query"
	authaction "gochen/auth/action"
)

// --- IReader ---

func (a *application[T, ID]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := a.require(ctx, authaction.OpRead); err != nil {
		return zero, err
	}
	return a.inner.Get(ctx, id)
}

func (a *application[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	if err := a.require(ctx, authaction.OpRead); err != nil {
		return false, err
	}
	return a.inner.Exists(ctx, id)
}

func (a *application[T, ID]) List(ctx context.Context, offset, limit int) ([]T, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return nil, err
	}
	return a.inner.List(ctx, offset, limit)
}

func (a *application[T, ID]) Count(ctx context.Context) (int64, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return 0, err
	}
	return a.inner.Count(ctx)
}

func (a *application[T, ID]) ListByQuery(ctx context.Context, quer *query.QueryRequest) ([]T, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return nil, err
	}
	return a.inner.ListByQuery(ctx, quer)
}

func (a *application[T, ID]) ListPage(ctx context.Context, request *query.PaginationOptions) (*query.PagedResult[T], error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return nil, err
	}
	return a.inner.ListPage(ctx, request)
}

func (a *application[T, ID]) CountByQuery(ctx context.Context, quer *query.QueryRequest) (int64, error) {
	if err := a.require(ctx, authaction.OpList); err != nil {
		return 0, err
	}
	return a.inner.CountByQuery(ctx, quer)
}
