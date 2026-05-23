package crud

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gochen/contextx"
	"gochen/db/query"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
)

type tenantQueryTestEntity struct {
	domaincrud.TenantEntity[int64]
	Name string
}

type mockTenantQueryableRepo struct {
	lastOpts query.QueryOptions
}

func (r *mockTenantQueryableRepo) Create(context.Context, *tenantQueryTestEntity) error { return nil }
func (r *mockTenantQueryableRepo) Update(context.Context, *tenantQueryTestEntity) error { return nil }
func (r *mockTenantQueryableRepo) Delete(context.Context, int64) error                  { return nil }
func (r *mockTenantQueryableRepo) Get(context.Context, int64) (*tenantQueryTestEntity, error) {
	return nil, errors.NewCode(errors.NotFound, "entity not found")
}
func (r *mockTenantQueryableRepo) List(context.Context, int, int) ([]*tenantQueryTestEntity, error) {
	return []*tenantQueryTestEntity{}, nil
}
func (r *mockTenantQueryableRepo) Count(context.Context) (int64, error) { return 0, nil }
func (r *mockTenantQueryableRepo) Exists(context.Context, int64) (bool, error) {
	return false, nil
}

func (r *mockTenantQueryableRepo) Query(ctx context.Context, opts query.QueryOptions) ([]*tenantQueryTestEntity, error) {
	r.lastOpts = opts
	return []*tenantQueryTestEntity{}, nil
}

func (r *mockTenantQueryableRepo) QueryOne(ctx context.Context, opts query.QueryOptions) (*tenantQueryTestEntity, error) {
	r.lastOpts = opts
	return nil, errors.NewCode(errors.NotFound, "entity not found")
}

func (r *mockTenantQueryableRepo) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	r.lastOpts = opts
	return 0, nil
}

var _ query.IQueryableRepository[*tenantQueryTestEntity, int64] = (*mockTenantQueryableRepo)(nil)

func TestTenantQueryWrapper_Query_OverridesTenantFilter(t *testing.T) {
	repo := &mockTenantQueryableRepo{}
	wrapper := NewTenantQueryWrapper[*tenantQueryTestEntity, int64](repo)

	ctx, err := contextx.WithTenantID(context.Background(), "tenant-1")
	require.NoError(t, err)

	_, err = wrapper.Query(ctx, query.QueryOptions{
		Filters: query.QueryFilters{
			"tenant_id": {{
				Op:    query.FilterOpEq,
				Value: query.StringValue("tenant-2"),
			}},
		},
	})
	require.NoError(t, err)

	expr, ok := repo.lastOpts.Filters.First("tenant_id")
	require.True(t, ok)
	assert.Equal(t, "tenant-1", expr.Value.String)
}

func TestTenantQueryWrapper_Count_UsesQueryableRepository(t *testing.T) {
	repo := &mockTenantQueryableRepo{}
	wrapper := NewTenantQueryWrapper[*tenantQueryTestEntity, int64](repo)

	ctx, err := contextx.WithTenantID(context.Background(), "tenant-1")
	require.NoError(t, err)

	_, err = wrapper.Count(ctx)
	require.NoError(t, err)

	expr, ok := repo.lastOpts.Filters.First("tenant_id")
	require.True(t, ok)
	assert.Equal(t, "tenant-1", expr.Value.String)
}

func TestTenantQueryWrapper_UsesResolverOption(t *testing.T) {
	repo := &mockTenantQueryableRepo{}
	wrapper := NewTenantQueryWrapper[*tenantQueryTestEntity, int64](repo, WithTenantQueryResolver[*tenantQueryTestEntity, int64](TenantResolverFunc(func(context.Context) (string, error) {
		return "tenant-from-option", nil
	})))

	_, err := wrapper.QueryCount(context.Background(), query.QueryOptions{})
	require.NoError(t, err)

	expr, ok := repo.lastOpts.Filters.First("tenant_id")
	require.True(t, ok)
	assert.Equal(t, "tenant-from-option", expr.Value.String)
}
