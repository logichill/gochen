package action_test

import (
	"context"
	"testing"

	secaction "gochen/app/security/action"

	"gochen/app/crud"
	"gochen/app/query"
	authaction "gochen/auth/action"
	domcrud "gochen/domain/crud"
	"gochen/errors"
	"gochen/testkit"
	"gochen/testkit/require"
)

type order struct {
	domcrud.Entity[int64]
	Name string
}

// denyAll 记录被校验过的动作码，并按预设放行/拒绝。
type recordingChecker struct {
	granted map[string]bool
	seen    []string
}

func (c *recordingChecker) RequireAction(_ context.Context, code string) error {
	c.seen = append(c.seen, code)
	if c.granted[code] {
		return nil
	}
	return errors.NewCode(errors.Forbidden, "action denied").WithContext("action", code)
}

func fullPolicy() authaction.OperationPolicy {
	return authaction.OperationPolicy{
		Create: "order:api:create",
		Read:   "order:api:read",
		Update: "order:api:update",
		Delete: "order:api:delete",
		List:   "order:api:list",
	}
}

func newApp(t *testing.T) crud.IApplication[*order, int64] {
	t.Helper()
	repo := testkit.NewMemoryRepository[*order, int64](testkit.NewInt64Sequence(1))
	app, err := crud.NewApplication[*order, int64](repo, nil, nil)
	require.NoError(t, err)
	return app
}

func TestNewValidatesWiring(t *testing.T) {
	app := newApp(t)
	checker := &recordingChecker{granted: map[string]bool{}}

	_, err := secaction.New[*order, int64](nil, checker, fullPolicy())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	_, err = secaction.New[*order, int64](app, nil, fullPolicy())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))

	// 非法权限码必须在装配期暴露，而不是等到请求期。
	_, err = secaction.New[*order, int64](app, checker, authaction.OperationPolicy{Create: "api:order"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestDeniedActionBlocksWrite(t *testing.T) {
	checker := &recordingChecker{granted: map[string]bool{}}
	secured, err := secaction.New[*order, int64](newApp(t), checker, fullPolicy())
	require.NoError(t, err)

	ctx := context.Background()
	err = secured.Create(ctx, &order{Name: "n1"})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
	require.Equal(t, []string{"order:api:create"}, checker.seen)

	// 被拒绝的写不得落库。
	count, err := secured.Repository().(domcrud.IQueryRepository[*order, int64]).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
}

func TestGrantedActionPassesThrough(t *testing.T) {
	checker := &recordingChecker{granted: map[string]bool{
		"order:api:create": true,
		"order:api:read":   true,
	}}
	secured, err := secaction.New[*order, int64](newApp(t), checker, fullPolicy())
	require.NoError(t, err)

	ctx := context.Background()
	entity := &order{Name: "n1"}
	require.NoError(t, secured.Create(ctx, entity))

	got, err := secured.Get(ctx, entity.GetID())
	require.NoError(t, err)
	require.Equal(t, "n1", got.Name)

	// 未授予的动作仍被拦截。
	require.True(t, errors.Is(secured.Delete(ctx, entity.GetID()), errors.Forbidden))
}

// 未在 policy 中配置的操作必须拒绝，绝不解释为"无需鉴权"（§4.1 fail-closed）。
func TestUnconfiguredOperationFailsClosed(t *testing.T) {
	checker := &recordingChecker{granted: map[string]bool{"order:api:create": true}}
	secured, err := secaction.New[*order, int64](newApp(t), checker, authaction.OperationPolicy{
		Create: "order:api:create",
		// Read / Update / Delete / List 未配置
	})
	require.NoError(t, err)

	ctx := context.Background()
	_, err = secured.Get(ctx, 1)
	require.True(t, errors.Is(err, errors.Forbidden))

	_, err = secured.List(ctx, 0, 10)
	require.True(t, errors.Is(err, errors.Forbidden))

	require.True(t, errors.Is(secured.Update(ctx, &order{}), errors.Forbidden))
	require.True(t, errors.Is(secured.Delete(ctx, 1), errors.Forbidden))

	// 未配置的操作不应触达 checker。
	require.Equal(t, 0, len(checker.seen))
}

func TestReadPathsMapToExpectedActions(t *testing.T) {
	checker := &recordingChecker{granted: map[string]bool{
		"order:api:read": true,
		"order:api:list": true,
	}}
	secured, err := secaction.New[*order, int64](newApp(t), checker, fullPolicy())
	require.NoError(t, err)

	ctx := context.Background()
	_, _ = secured.Get(ctx, 1)
	_, _ = secured.Exists(ctx, 1)
	_, _ = secured.List(ctx, 0, 10)
	_, _ = secured.Count(ctx)
	_, _ = secured.ListByQuery(ctx, &query.QueryRequest{})
	_, _ = secured.CountByQuery(ctx, &query.QueryRequest{})

	require.Equal(t, []string{
		"order:api:read",
		"order:api:read",
		"order:api:list",
		"order:api:list",
		"order:api:list",
		"order:api:list",
	}, checker.seen)
}

// R5 关键回归：装饰 IApplication（宽接口）不得丢失底层可选能力接口。
func TestDecoratorPreservesOptionalRepositoryCapabilities(t *testing.T) {
	checker := &recordingChecker{granted: map[string]bool{}}
	secured, err := secaction.New[*order, int64](newApp(t), checker, fullPolicy())
	require.NoError(t, err)

	_, ok := secured.Repository().(domcrud.IQueryRepository[*order, int64])
	require.True(t, ok, "IQueryRepository 断言必须仍然成立")

	queryRepo, ok := secured.QueryRepository()
	require.True(t, ok, "QueryRepository() 必须仍可用")
	require.NotNil(t, queryRepo)

	require.NotNil(t, secured.Config())
}
