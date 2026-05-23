package crud

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gochen/contextx"
	"gochen/db/query"
	domaincrud "gochen/domain/crud"
	"gochen/errors"
)

type testTenantUser struct {
	domaincrud.TenantEntity[int64]
	Name string
}

type mockTenantRepository struct {
	entities map[int64]*testTenantUser
}

func newMockTenantRepository() *mockTenantRepository {
	return &mockTenantRepository{entities: make(map[int64]*testTenantUser)}
}

func cloneTestTenantUser(u *testTenantUser) *testTenantUser {
	if u == nil {
		return nil
	}
	c := *u
	return &c
}

func (r *mockTenantRepository) Create(context.Context, *testTenantUser) error {
	panic("Create should be implemented")
}

func (r *mockTenantRepository) Update(context.Context, *testTenantUser) error {
	panic("Update should be implemented")
}

func (r *mockTenantRepository) Delete(context.Context, int64) error {
	panic("Delete should be implemented")
}

func (r *mockTenantRepository) Get(context.Context, int64) (*testTenantUser, error) {
	panic("Get should be implemented")
}

func (r *mockTenantRepository) List(context.Context, int, int) ([]*testTenantUser, error) {
	panic("List should be implemented")
}

func (r *mockTenantRepository) Count(context.Context) (int64, error) {
	panic("Count should be implemented")
}

func (r *mockTenantRepository) Exists(context.Context, int64) (bool, error) {
	panic("Exists should be implemented")
}

var _ domaincrud.IRepository[*testTenantUser, int64] = (*mockTenantRepository)(nil)

func (r *mockTenantRepository) CreateStored(ctx context.Context, e *testTenantUser) error {
	if _, exists := r.entities[e.GetID()]; exists {
		return errors.NewCode(errors.Conflict, "entity already exists")
	}
	r.entities[e.GetID()] = cloneTestTenantUser(e)
	return nil
}

func (r *mockTenantRepository) UpdateStored(ctx context.Context, e *testTenantUser) error {
	if _, exists := r.entities[e.GetID()]; !exists {
		return errors.NewCode(errors.NotFound, "entity not found")
	}
	r.entities[e.GetID()] = cloneTestTenantUser(e)
	return nil
}

func (r *mockTenantRepository) DeleteStored(ctx context.Context, id int64) error {
	delete(r.entities, id)
	return nil
}

func (r *mockTenantRepository) GetStored(ctx context.Context, id int64) (*testTenantUser, error) {
	e, exists := r.entities[id]
	if !exists {
		return nil, errors.NewCode(errors.NotFound, "entity not found")
	}
	return cloneTestTenantUser(e), nil
}

func (r *mockTenantRepository) ListStored(ctx context.Context, offset, limit int) ([]*testTenantUser, error) {
	result := make([]*testTenantUser, 0)
	for _, e := range r.entities {
		result = append(result, cloneTestTenantUser(e))
	}
	return result, nil
}

func (r *mockTenantRepository) CountStored(ctx context.Context) (int64, error) {
	return int64(len(r.entities)), nil
}

func (r *mockTenantRepository) ExistsStored(ctx context.Context, id int64) (bool, error) {
	_, exists := r.entities[id]
	return exists, nil
}

type tenantRepositoryAdapter struct{ *mockTenantRepository }

func (r *tenantRepositoryAdapter) Create(ctx context.Context, e *testTenantUser) error {
	return r.CreateStored(ctx, e)
}

func (r *tenantRepositoryAdapter) Update(ctx context.Context, e *testTenantUser) error {
	return r.UpdateStored(ctx, e)
}

func (r *tenantRepositoryAdapter) Delete(ctx context.Context, id int64) error {
	return r.DeleteStored(ctx, id)
}

func (r *tenantRepositoryAdapter) Get(ctx context.Context, id int64) (*testTenantUser, error) {
	return r.GetStored(ctx, id)
}

func (r *tenantRepositoryAdapter) List(ctx context.Context, offset, limit int) ([]*testTenantUser, error) {
	return r.ListStored(ctx, offset, limit)
}

func (r *tenantRepositoryAdapter) Count(ctx context.Context) (int64, error) {
	return r.CountStored(ctx)
}

func (r *tenantRepositoryAdapter) Exists(ctx context.Context, id int64) (bool, error) {
	return r.ExistsStored(ctx, id)
}

type mockTenantRepositoryWithTenant struct {
	*tenantRepositoryAdapter
}

func newMockTenantRepositoryWithTenant() *mockTenantRepositoryWithTenant {
	return &mockTenantRepositoryWithTenant{tenantRepositoryAdapter: &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}}
}

func (r *mockTenantRepositoryWithTenant) ListByTenant(ctx context.Context, tenantID string, offset, limit int) ([]*testTenantUser, error) {
	ids := make([]int64, 0, len(r.entities))
	for id := range r.entities {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	filtered := make([]*testTenantUser, 0, len(ids))
	for _, id := range ids {
		e := r.entities[id]
		if e != nil && e.GetTenantID() == tenantID {
			filtered = append(filtered, cloneTestTenantUser(e))
		}
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if offset >= len(filtered) || limit == 0 {
		return []*testTenantUser{}, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], nil
}

func (r *mockTenantRepositoryWithTenant) CountByTenant(ctx context.Context, tenantID string) (int64, error) {
	var n int64
	for _, e := range r.entities {
		if e != nil && e.GetTenantID() == tenantID {
			n++
		}
	}
	return n, nil
}

var _ domaincrud.ITenantRepository[*testTenantUser, int64] = (*mockTenantRepositoryWithTenant)(nil)

type mockTenantQueryableRepository struct {
	*tenantRepositoryAdapter
	lastOpts query.QueryOptions
}

func newMockTenantQueryableRepository() *mockTenantQueryableRepository {
	return &mockTenantQueryableRepository{
		tenantRepositoryAdapter: &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()},
	}
}

func (r *mockTenantQueryableRepository) Query(ctx context.Context, opts query.QueryOptions) ([]*testTenantUser, error) {
	r.lastOpts = opts
	return r.List(ctx, opts.Offset, opts.Limit)
}

func (r *mockTenantQueryableRepository) QueryOne(ctx context.Context, opts query.QueryOptions) (*testTenantUser, error) {
	r.lastOpts = opts
	return nil, errors.NewCode(errors.NotFound, "entity not found")
}

func (r *mockTenantQueryableRepository) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	r.lastOpts = opts
	return r.Count(ctx)
}

var _ query.IQueryableRepository[*testTenantUser, int64] = (*mockTenantQueryableRepository)(nil)

type mockTransactionalTenantRepository struct {
	*tenantRepositoryAdapter
	beginCalled    bool
	commitCalled   bool
	rollbackCalled bool
}

func newMockTransactionalTenantRepository() *mockTransactionalTenantRepository {
	return &mockTransactionalTenantRepository{
		tenantRepositoryAdapter: &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()},
	}
}

func (r *mockTransactionalTenantRepository) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

func (r *mockTransactionalTenantRepository) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	r.beginCalled = true
	txCtx := context.WithValue(ctx, "tx", true)
	return contextx.NewTxScope(txCtx, true)
}

func (r *mockTransactionalTenantRepository) Commit(tx contextx.TxScope) error {
	_ = tx
	r.commitCalled = true
	return nil
}

func (r *mockTransactionalTenantRepository) Rollback(tx contextx.TxScope) error {
	_ = tx
	r.rollbackCalled = true
	return nil
}

func TestTenantAwareWrapper_Create(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	user := &testTenantUser{}
	user.ID = 1
	user.Name = "test"

	err := wrapper.Create(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, "tenant-1", user.GetTenantID())

	stored, err := repo.Get(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "tenant-1", stored.GetTenantID())
}

func TestTenantAwareWrapper_Create_NoTenantID(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	user := &testTenantUser{}
	user.ID = 1

	err := wrapper.Create(context.Background(), user)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.InvalidInput))
}

func TestTenantAwareWrapper_Get(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	user := &testTenantUser{}
	user.ID = 1
	user.Name = "test"
	user.TenantID = "tenant-1"
	_ = repo.Create(ctx, user)

	got, err := wrapper.Get(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "test", got.Name)
}

func TestTenantAwareWrapper_Get_CrossTenant(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	user := &testTenantUser{}
	user.ID = 1
	user.Name = "test"
	user.TenantID = "tenant-1"
	_ = repo.Create(context.Background(), user)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-2")
	_, err := wrapper.Get(ctx, 1)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.NotFound))
}

func TestTenantAwareWrapper_Update_CrossTenant(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	user := &testTenantUser{}
	user.ID = 1
	user.Name = "test"
	user.TenantID = "tenant-1"
	_ = repo.Create(context.Background(), user)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-2")
	user.Name = "updated"

	err := wrapper.Update(ctx, user)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Forbidden))
}

func TestTenantAwareWrapper_Update_ValidatesStoredTenant(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	user := &testTenantUser{}
	user.ID = 1
	user.Name = "test"
	user.TenantID = "tenant-1"
	_ = repo.Create(context.Background(), user)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-2")
	user.TenantID = "tenant-2"
	user.Name = "updated"

	err := wrapper.Update(ctx, user)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Forbidden))
}

func TestTenantAwareWrapper_Delete_CrossTenant(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	user := &testTenantUser{}
	user.ID = 1
	user.TenantID = "tenant-1"
	_ = repo.Create(context.Background(), user)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-2")
	err := wrapper.Delete(ctx, 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Forbidden))

	_, err = repo.Get(context.Background(), 1)
	require.NoError(t, err)
}

func TestTenantAwareWrapper_Exists(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	user := &testTenantUser{}
	user.ID = 1
	user.TenantID = "tenant-1"
	_ = repo.Create(context.Background(), user)

	ctx1, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	exists, err := wrapper.Exists(ctx1, 1)
	require.NoError(t, err)
	assert.True(t, exists)

	ctx2, _ := contextx.WithTenantID(context.Background(), "tenant-2")
	exists, err = wrapper.Exists(ctx2, 1)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTenantAwareWrapper_List_FiltersByTenantWhenInnerIsNotTenantAware(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	_ = repo.Create(context.Background(), &testTenantUser{TenantEntity: domaincrud.TenantEntity[int64]{Entity: domaincrud.Entity[int64]{ID: 1}, TenantID: "tenant-1"}})
	_ = repo.Create(context.Background(), &testTenantUser{TenantEntity: domaincrud.TenantEntity[int64]{Entity: domaincrud.Entity[int64]{ID: 2}, TenantID: "tenant-2"}})

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	_, err := wrapper.List(ctx, 0, 100)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Unsupported))
}

func TestTenantAwareWrapper_Count_RequiresTenantRepository(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	_, err := wrapper.Count(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Unsupported))
}

func TestTenantAwareWrapper_List_UsesTenantRepository(t *testing.T) {
	repo := newMockTenantRepositoryWithTenant()
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	_ = repo.Create(context.Background(), &testTenantUser{TenantEntity: domaincrud.TenantEntity[int64]{Entity: domaincrud.Entity[int64]{ID: 1}, TenantID: "tenant-1"}})
	_ = repo.Create(context.Background(), &testTenantUser{TenantEntity: domaincrud.TenantEntity[int64]{Entity: domaincrud.Entity[int64]{ID: 2}, TenantID: "tenant-2"}})
	_ = repo.Create(context.Background(), &testTenantUser{TenantEntity: domaincrud.TenantEntity[int64]{Entity: domaincrud.Entity[int64]{ID: 3}, TenantID: "tenant-1"}})

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	list, err := wrapper.List(ctx, 0, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "tenant-1", list[0].GetTenantID())
	assert.Equal(t, "tenant-1", list[1].GetTenantID())
}

func TestTenantAwareWrapper_List_UsesQueryableRepositoryWhenAvailable(t *testing.T) {
	repo := newMockTenantQueryableRepository()
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	_, err := wrapper.List(ctx, 3, 7)
	require.NoError(t, err)

	expr, ok := repo.lastOpts.Filters.First("tenant_id")
	require.True(t, ok)
	assert.Equal(t, "tenant-1", expr.Value.String)
	assert.Equal(t, 3, repo.lastOpts.Offset)
	assert.Equal(t, 7, repo.lastOpts.Limit)
}

func TestTenantAwareWrapper_Query_OverridesTenantFilter(t *testing.T) {
	repo := newMockTenantQueryableRepository()
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	_, err := wrapper.Query(ctx, query.QueryOptions{
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

func TestTenantAwareWrapper_Count_UsesQueryableRepositoryWhenAvailable(t *testing.T) {
	repo := newMockTenantQueryableRepository()
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	ctx, _ := contextx.WithTenantID(context.Background(), "tenant-1")
	_, err := wrapper.Count(ctx)
	require.NoError(t, err)

	expr, ok := repo.lastOpts.Filters.First("tenant_id")
	require.True(t, ok)
	assert.Equal(t, "tenant-1", expr.Value.String)
}

func TestTenantAwareWrapper_UsesConfiguredResolver(t *testing.T) {
	SetTenantResolver(TenantResolverFunc(func(context.Context) (string, error) {
		return "fixed-tenant", nil
	}))
	defer SetTenantResolver(nil)

	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	user := &testTenantUser{}
	user.ID = 1
	user.Name = "fixed"

	err := wrapper.Create(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "fixed-tenant", user.GetTenantID())
}

func TestResolveTenantID_UsesContextTenant(t *testing.T) {
	ctx, err := contextx.WithTenantID(context.Background(), "tenant-context")
	require.NoError(t, err)

	tenantID, err := ResolveTenantID(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tenant-context", tenantID)
}

func TestTenantAwareWrapper_UsesResolverOption(t *testing.T) {
	repo := &tenantRepositoryAdapter{mockTenantRepository: newMockTenantRepository()}
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo, WithTenantResolver[*testTenantUser, int64](TenantResolverFunc(func(context.Context) (string, error) {
		return "option-tenant", nil
	})))

	user := &testTenantUser{}
	user.ID = 2
	user.Name = "option"

	err := wrapper.Create(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, "option-tenant", user.GetTenantID())
}

func TestTenantAwareWrapper_WithinTx_DelegatesTransactions(t *testing.T) {
	repo := newMockTransactionalTenantRepository()
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)

	var called bool
	err := wrapper.WithinTx(context.Background(), func(txCtx context.Context) error {
		called = true
		require.NotNil(t, txCtx)
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.True(t, repo.beginCalled)
	assert.True(t, repo.commitCalled)
	assert.False(t, repo.rollbackCalled)
}
