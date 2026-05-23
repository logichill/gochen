package repo

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	auth "gochen/auth"
	"gochen/contextx"
	"gochen/db"
	"gochen/db/orm"
	"gochen/domain/access"
	"gochen/errors"
)

type guardedWriteModel struct {
	lastSaveOpts         orm.QueryOptions
	lastDeleteOpts       orm.QueryOptions
	lastUpdateValuesOpts orm.QueryOptions
	created              any
	saveAffected         int64
	deleteAffected       int64
	updateAffected       int64
	rawCount             int64
	scopedVisible        bool
	visibleEntity        *scopedRepoEntity
}

type recordingWriteAuditRecorder struct {
	records []access.WriteAuditRecord
}

func (r *recordingWriteAuditRecorder) RecordWriteAudit(_ context.Context, record access.WriteAuditRecord) {
	r.records = append(r.records, record)
}

type guardedWriteSession struct {
	model      orm.IModel
	dispatcher contextx.IAfterCommitDispatcher
}

func (m *guardedWriteModel) Meta() *orm.ModelMeta           { return &orm.ModelMeta{Table: "scoped_entities"} }
func (m *guardedWriteModel) Capabilities() orm.Capabilities { return nil }
func (m *guardedWriteModel) First(ctx context.Context, dest any, opts ...orm.QueryOption) error {
	_ = ctx
	_ = opts
	if !m.scopedVisible {
		return errors.NewCode(errors.NotFound, "record not found")
	}
	switch typed := dest.(type) {
	case **scopedRepoEntity:
		if m.visibleEntity == nil {
			return errors.NewCode(errors.NotFound, "record not found")
		}
		*typed = m.visibleEntity
	case *scopedRepoEntity:
		if m.visibleEntity == nil {
			return errors.NewCode(errors.NotFound, "record not found")
		}
		*typed = *m.visibleEntity
	}
	return nil
}
func (m *guardedWriteModel) Find(context.Context, any, ...orm.QueryOption) error { return nil }
func (m *guardedWriteModel) Count(ctx context.Context, opts ...orm.QueryOption) (int64, error) {
	_ = ctx
	collected := orm.CollectQueryOptions(opts...)
	if hasWhereExpr(collected, "managed_scope_id = ?") {
		if m.scopedVisible {
			return 1, nil
		}
		return 0, nil
	}
	return m.rawCount, nil
}
func (m *guardedWriteModel) Create(ctx context.Context, entities ...any) error {
	_ = ctx
	if len(entities) > 0 {
		m.created = entities[0]
	}
	return nil
}
func (s *guardedWriteSession) Capabilities() orm.Capabilities           { return nil }
func (s *guardedWriteSession) WithContext(context.Context) orm.IOrm     { return s }
func (s *guardedWriteSession) Model(*orm.ModelMeta) (orm.IModel, error) { return s.model, nil }
func (s *guardedWriteSession) Begin(context.Context) (orm.IOrmSession, error) {
	return nil, errors.NewCode(errors.Unsupported, "not implemented")
}
func (s *guardedWriteSession) BeginTx(context.Context, *sql.TxOptions) (orm.IOrmSession, error) {
	return nil, errors.NewCode(errors.Unsupported, "not implemented")
}
func (s *guardedWriteSession) Database() db.IDatabase { return nil }
func (s *guardedWriteSession) Commit() error          { return nil }
func (s *guardedWriteSession) Rollback() error        { return nil }
func (s *guardedWriteSession) AfterCommitDispatcher() contextx.IAfterCommitDispatcher {
	if s.dispatcher == nil {
		s.dispatcher = contextx.NewAfterCommitDispatcher()
	}
	return s.dispatcher
}
func (m *guardedWriteModel) Save(context.Context, any, ...orm.QueryOption) error { panic("not used") }
func (m *guardedWriteModel) SaveWithResult(ctx context.Context, entity any, opts ...orm.QueryOption) (sql.Result, error) {
	_ = ctx
	_ = entity
	m.lastSaveOpts = orm.CollectQueryOptions(opts...)
	return fakeResult(m.saveAffected), nil
}
func (m *guardedWriteModel) UpdateValues(context.Context, map[string]any, ...orm.QueryOption) error {
	panic("not used")
}
func (m *guardedWriteModel) UpdateValuesWithResult(ctx context.Context, values map[string]any, opts ...orm.QueryOption) (sql.Result, error) {
	_ = ctx
	_ = values
	m.lastUpdateValuesOpts = orm.CollectQueryOptions(opts...)
	return fakeResult(m.updateAffected), nil
}
func (m *guardedWriteModel) Delete(context.Context, ...orm.QueryOption) error { panic("not used") }
func (m *guardedWriteModel) DeleteWithResult(ctx context.Context, opts ...orm.QueryOption) (sql.Result, error) {
	_ = ctx
	m.lastDeleteOpts = orm.CollectQueryOptions(opts...)
	return fakeResult(m.deleteAffected), nil
}
func (m *guardedWriteModel) Association(any, string) orm.IAssociation { return nil }

func TestRepo_CreateWithConstraint_AlignsManagedScope(t *testing.T) {
	model := &guardedWriteModel{}
	repository := &Repo[*scopedRepoEntity, int64]{model: model, resourceKind: "document"}
	entity := &scopedRepoEntity{ID: 7}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   701,
		VisibleScopeIDs: []int64{701},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	constraint := auth.WriteConstraintFromDecision(auth.AllowDecision(auth.Resource{Kind: "document", ManagedScopeID: 701}))

	err = repository.CreateWithConstraint(ctx, entity, constraint)
	require.NoError(t, err)
	require.Equal(t, int64(701), entity.ManagedScopeID)
	require.Same(t, entity, model.created)
}

func TestRepo_UpdateWithConstraint_AppliesRevisionAndManagedScopePredicates(t *testing.T) {
	model := &guardedWriteModel{
		saveAffected:  1,
		scopedVisible: true,
		visibleEntity: &scopedRepoEntity{ID: 7, Version: 5, ManagedScopeID: 701},
		rawCount:      1,
	}
	repository := &Repo[*scopedRepoEntity, int64]{model: model, resourceKind: "document"}
	entity := &scopedRepoEntity{ID: 7, Version: 5}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   701,
		VisibleScopeIDs: []int64{701},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)

	constraint := auth.WriteConstraintFromDecision(auth.AllowDecision(auth.Resource{
		Kind:           "document",
		ID:             "7",
		ManagedScopeID: 701,
		Revision:       "5",
	}))

	err = repository.UpdateWithConstraint(ctx, entity, constraint)
	require.NoError(t, err)
	requireCondition(t, model.lastSaveOpts.Where, "id = ?", int64(7))
	requireCondition(t, model.lastSaveOpts.Where, "managed_scope_id = ?", int64(701))
	requireCondition(t, model.lastSaveOpts.Where, "version = ?", uint64(5))
}

func TestRepo_UpdateWithConstraint_RecordsWriteAudit(t *testing.T) {
	model := &guardedWriteModel{
		saveAffected:  1,
		scopedVisible: true,
		visibleEntity: &scopedRepoEntity{ID: 7, Version: 5, ManagedScopeID: 701},
		rawCount:      1,
	}
	repository := &Repo[*scopedRepoEntity, int64]{model: model, resourceKind: "document"}
	entity := &scopedRepoEntity{ID: 7, Version: 5}
	recorder := &recordingWriteAuditRecorder{}
	ctx := access.WithWriteAuditRecorder(context.Background(), recorder)
	ctx, err := auth.WithDataScope(ctx, auth.DataScope{ActiveScopeID: 701, VisibleScopeIDs: []int64{701}, Mode: auth.ScopeModeScoped})
	require.NoError(t, err)

	constraint := auth.WriteConstraintFromDecision(auth.AllowDecision(auth.Resource{Kind: "document", ID: "7", ManagedScopeID: 701, Revision: "5"}))
	err = repository.UpdateWithConstraint(ctx, entity, constraint)
	require.NoError(t, err)
	require.Len(t, recorder.records, 1)
	require.Equal(t, "update", recorder.records[0].Operation)
	require.Equal(t, int64(701), recorder.records[0].Resources[0].ManagedScopeID)
}

func TestRepo_UpdateWithConstraint_RejectsManagedScopeMismatch(t *testing.T) {
	model := &guardedWriteModel{saveAffected: 1}
	repository := &Repo[*scopedRepoEntity, int64]{model: model, resourceKind: "document"}
	entity := &scopedRepoEntity{ID: 7, Version: 5, ManagedScopeID: 999}
	ctx, err := auth.WithDataScope(context.Background(), auth.DataScope{
		ActiveScopeID:   10,
		VisibleScopeIDs: []int64{999, 1000},
		Mode:            auth.ScopeModeScoped,
	})
	require.NoError(t, err)
	constraint := auth.WriteConstraintFromDecision(auth.AllowDecision(auth.Resource{Kind: "document", ID: "7", ManagedScopeID: 701, Revision: "5"}))

	err = repository.UpdateWithConstraint(ctx, entity, constraint)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Forbidden))
}
