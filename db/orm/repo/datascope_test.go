package repo

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	auth "gochen/auth"
	"gochen/db"
	"gochen/db/orm"
	"gochen/db/query"
	"gochen/domain/access"
	"gochen/errors"
)

type scopedRepoEntity struct {
	ID             int64  `json:"id"`
	Version        uint64 `json:"version"`
	ManagedScopeID int64  `json:"managed_scope_id"`
	OwnerID        string `json:"owner_id"`
	Name           string `json:"name"`
}

func (e *scopedRepoEntity) GetID() int64       { return e.ID }
func (e *scopedRepoEntity) GetVersion() uint64 { return e.Version }

type inferredScopedRepoEntity struct {
	ID             int64
	Version        uint64
	ManagedScopeID int64
	OwnerID        string
	Name           string
}

func (e *inferredScopedRepoEntity) GetID() int64       { return e.ID }
func (e *inferredScopedRepoEntity) GetVersion() uint64 { return e.Version }

type inferredTenantRepoEntity struct {
	ID       int64
	TenantID string
	Name     string
}

func (e *inferredTenantRepoEntity) GetID() int64       { return e.ID }
func (e *inferredTenantRepoEntity) GetVersion() uint64 { return 0 }

type recursiveScopedEntity struct {
	ID             int64                  `json:"id"`
	ManagedScopeID int64                  `json:"managed_scope_id"`
	Parent         *recursiveScopedEntity `json:"parent,omitempty"`
}

func (e *recursiveScopedEntity) GetID() int64       { return e.ID }
func (e *recursiveScopedEntity) GetVersion() uint64 { return 0 }

type customTaggedScopedEntity struct {
	ID           int64  `gorm:"column:id"`
	ManagedScope int64  `gorm:"column:scope_ref"`
	Owner        string `gorm:"column:owner_ref"`
	Revision     uint64 `gorm:"column:revision_no"`
}

func (e *customTaggedScopedEntity) GetID() int64       { return e.ID }
func (e *customTaggedScopedEntity) GetVersion() uint64 { return e.Revision }

type noAuthzFieldEntity struct {
	ID   int64  `gorm:"column:id"`
	Name string `gorm:"column:name"`
}

func (e *noAuthzFieldEntity) GetID() int64       { return e.ID }
func (e *noAuthzFieldEntity) GetVersion() uint64 { return 0 }

type unsafeTaggedScopedEntity struct {
	ID             int64  `gorm:"column:id"`
	ManagedScopeID int64  `gorm:"column:scope-ref"`
	OwnerID        string `gorm:"column:owner_ref"`
	Version        uint64 `gorm:"column:version"`
}

func (e *unsafeTaggedScopedEntity) GetID() int64       { return e.ID }
func (e *unsafeTaggedScopedEntity) GetVersion() uint64 { return e.Version }

type capturingDataScopeModel struct {
	lastFindOpts  orm.QueryOptions
	lastFirstOpts orm.QueryOptions
	lastSaveOpts  orm.QueryOptions
	created       any
	firstCalls    int
}

func (m *capturingDataScopeModel) Meta() *orm.ModelMeta {
	return &orm.ModelMeta{Table: "scoped_entities"}
}
func (m *capturingDataScopeModel) Capabilities() orm.Capabilities { return nil }
func (m *capturingDataScopeModel) First(ctx context.Context, dest any, opts ...orm.QueryOption) error {
	_ = ctx
	_ = dest
	m.firstCalls++
	m.lastFirstOpts = orm.CollectQueryOptions(opts...)
	return nil
}
func (m *capturingDataScopeModel) Find(ctx context.Context, dest any, opts ...orm.QueryOption) error {
	_ = ctx
	_ = dest
	m.lastFindOpts = orm.CollectQueryOptions(opts...)
	return nil
}
func (m *capturingDataScopeModel) Count(context.Context, ...orm.QueryOption) (int64, error) {
	return 0, nil
}
func (m *capturingDataScopeModel) Create(ctx context.Context, entities ...any) error {
	_ = ctx
	if len(entities) > 0 {
		m.created = entities[0]
	}
	return nil
}
func (m *capturingDataScopeModel) Save(ctx context.Context, entity any, opts ...orm.QueryOption) error {
	_ = ctx
	_ = entity
	m.lastSaveOpts = orm.CollectQueryOptions(opts...)
	return nil
}
func (m *capturingDataScopeModel) UpdateValues(context.Context, map[string]any, ...orm.QueryOption) error {
	panic("not used")
}
func (m *capturingDataScopeModel) Delete(context.Context, ...orm.QueryOption) error {
	panic("not used")
}
func (m *capturingDataScopeModel) Association(any, string) orm.IAssociation { return nil }

type staticRepoOrm struct {
	model orm.IModel
}

func (s *staticRepoOrm) Capabilities() orm.Capabilities { return nil }
func (s *staticRepoOrm) WithContext(context.Context) orm.IOrm {
	return s
}
func (s *staticRepoOrm) Model(*orm.ModelMeta) (orm.IModel, error) {
	return s.model, nil
}
func (s *staticRepoOrm) Begin(context.Context) (orm.IOrmSession, error) {
	return nil, errors.NewCode(errors.Unsupported, "not implemented")
}
func (s *staticRepoOrm) BeginTx(context.Context, *sql.TxOptions) (orm.IOrmSession, error) {
	return nil, errors.NewCode(errors.Unsupported, "not implemented")
}
func (s *staticRepoOrm) Database() db.IDatabase { return nil }

func TestInferDataScopeSchema_HandlesIDInitialismsWithoutTags(t *testing.T) {
	schema := inferDataScopeSchema[*inferredScopedRepoEntity](accessColumns{})

	require.Equal(t, "managed_scope_id", schema.managedScope.column)
	require.Equal(t, "owner_id", schema.ownerID.column)
	require.Equal(t, "version", schema.version.column)
}

func TestRepo_Query_AppliesManagedScopeFilters(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*scopedRepoEntity, int64]{model: model}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{101, 202},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	_, err = repository.Query(ctx, query.QueryOptions{})
	require.NoError(t, err)
	requireCondition(t, model.lastFindOpts.Where, "managed_scope_id IN ?", []any{int64(101), int64(202)})
}

func TestRepo_Query_AppliesManagedScopeFiltersFromInferredIDField(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*inferredScopedRepoEntity, int64]{model: model}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{101, 202},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	_, err = repository.Query(ctx, query.QueryOptions{})
	require.NoError(t, err)
	requireCondition(t, model.lastFindOpts.Where, "managed_scope_id IN ?", []any{int64(101), int64(202)})
}

func TestRepo_Query_AppliesTenantFilterFromInferredTenantIDField(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*inferredTenantRepoEntity, int64]{model: model}
	ctx, err := auth.WithTenantID(context.Background(), "tenant-a")
	require.NoError(t, err)

	_, err = repository.Query(ctx, query.QueryOptions{})
	require.NoError(t, err)
	requireCondition(t, model.lastFindOpts.Where, "tenant_id = ?", "tenant-a")
}

func TestRepo_Query_AppliesAppAccessManagedScopeFilters(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*scopedRepoEntity, int64]{model: model}
	ctx := access.WithDataScope(context.Background(), access.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{303, 404},
		Mode:            access.ScopeModeScoped,
	})

	_, err := repository.Query(ctx, query.QueryOptions{})
	require.NoError(t, err)
	requireCondition(t, model.lastFindOpts.Where, "managed_scope_id IN ?", []any{int64(303), int64(404)})
}

func TestRepo_Query_UsesExplicitAccessColumns(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*customTaggedScopedEntity, int64]{model: model}
	WithAccessColumns[*customTaggedScopedEntity, int64]("scope_ref", "owner_ref", "revision_no")(repository)
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{505, 606},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	_, err = repository.Query(ctx, query.QueryOptions{})
	require.NoError(t, err)
	requireCondition(t, model.lastFindOpts.Where, "scope_ref IN ?", []any{int64(505), int64(606)})
}

func TestNewRepo_FailsFastOnUnsafeAccessColumns(t *testing.T) {
	model := &capturingDataScopeModel{}
	ormEngine := &staticRepoOrm{model: model}

	_, err := NewRepo[*customTaggedScopedEntity, int64](
		ormEngine,
		"scoped_entities",
		WithAccessColumns[*customTaggedScopedEntity, int64]("scope-ref", "owner_ref", "revision_no"),
	)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestNewRepo_FailsFastOnUnsafeTagDerivedColumns(t *testing.T) {
	model := &capturingDataScopeModel{}
	ormEngine := &staticRepoOrm{model: model}

	_, err := NewRepo[*unsafeTaggedScopedEntity, int64](ormEngine, "scoped_entities")
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestRepo_Create_InjectsSingleManagedScopeIntoEntity(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*scopedRepoEntity, int64]{model: model}
	entity := &scopedRepoEntity{ID: 1, Name: "demo"}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   909,
		VisibleScopeIDs: []int64{909},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	err = repository.Create(ctx, entity)
	require.NoError(t, err)
	require.Equal(t, int64(909), entity.ManagedScopeID)
	require.Same(t, entity, model.created)
}

func TestRepo_Create_MultiScopeRequiresExplicitManagedScope(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*scopedRepoEntity, int64]{model: model}
	entity := &scopedRepoEntity{ID: 1, Name: "demo"}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{1001, 1002},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	err = repository.Create(ctx, entity)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestRepo_Update_AppliesManagedScopeFiltersAndNormalizesEntity(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*scopedRepoEntity, int64]{model: model}
	entity := &scopedRepoEntity{ID: 7, ManagedScopeID: 888}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{707},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	err = repository.Update(ctx, entity)
	require.NoError(t, err)
	require.Equal(t, int64(707), entity.ManagedScopeID)
	requireCondition(t, model.lastSaveOpts.Where, "managed_scope_id = ?", int64(707))
	requireCondition(t, model.lastSaveOpts.Where, "id = ?", int64(7))
}

func TestRepo_Get_ScopedEntityRequiresDataScope(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*scopedRepoEntity, int64]{model: model}

	_, err := repository.Get(context.Background(), 1)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
	require.Equal(t, 0, model.firstCalls)
}

func TestRepo_Query_IgnoresRecursiveStructCycles(t *testing.T) {
	model := &capturingDataScopeModel{}
	repository := &Repo[*recursiveScopedEntity, int64]{model: model}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{1212},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	_, err = repository.Query(ctx, query.QueryOptions{})
	require.NoError(t, err)
	requireCondition(t, model.lastFindOpts.Where, "managed_scope_id = ?", int64(1212))
}

func requireCondition(t *testing.T, conditions []orm.Condition, expr string, want any) {
	t.Helper()
	for _, condition := range conditions {
		if condition.Expr != expr {
			continue
		}
		if want == nil {
			require.Len(t, condition.Args, 0)
			return
		}
		require.Len(t, condition.Args, 1)
		require.Equal(t, want, condition.Args[0])
		return
	}
	t.Fatalf("condition %quer not found in %#v", expr, conditions)
}
