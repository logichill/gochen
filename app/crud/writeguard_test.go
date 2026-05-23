package crud

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/contextx"
	"gochen/domain/access"
	"gochen/errors"
)

type guardedRecordingRepo struct {
	recordingRepo
	createConstraint access.WriteConstraint
	updateConstraint access.WriteConstraint
	deleteConstraint access.WriteConstraint
	created          testEntity
	updated          testEntity
	deletedID        int64
}

func (r *guardedRecordingRepo) CreateWithConstraint(ctx context.Context, e testEntity, constraint access.WriteConstraint) error {
	_ = ctx
	r.created = e
	r.createConstraint = constraint
	return nil
}

func (r *guardedRecordingRepo) UpdateWithConstraint(ctx context.Context, e testEntity, constraint access.WriteConstraint) error {
	_ = ctx
	r.updated = e
	r.updateConstraint = constraint
	return nil
}

func (r *guardedRecordingRepo) DeleteWithConstraint(ctx context.Context, id int64, constraint access.WriteConstraint) error {
	_ = ctx
	r.deletedID = id
	r.deleteConstraint = constraint
	return nil
}

var _ access.IWriteConstraintRepository[testEntity, int64] = (*guardedRecordingRepo)(nil)

type guardedBatchFallbackRepo struct {
	guardedRecordingRepo
	updateConstraints []access.WriteConstraint
}

func (r *guardedBatchFallbackRepo) UpdateWithConstraint(ctx context.Context, e testEntity, constraint access.WriteConstraint) error {
	_ = ctx
	r.updated = e
	r.updateConstraint = constraint
	r.updateConstraints = append(r.updateConstraints, constraint)
	return nil
}

func (r *guardedBatchFallbackRepo) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

func (r *guardedBatchFallbackRepo) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	return contextx.NewTxScope(ctx, true)
}

func (r *guardedBatchFallbackRepo) Commit(contextx.TxScope) error   { return nil }
func (r *guardedBatchFallbackRepo) Rollback(contextx.TxScope) error { return nil }

func TestApplication_UpdateWithConstraint_UsesConstrainedRepository(t *testing.T) {
	repo := &guardedRecordingRepo{}
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	require.NoError(t, err)
	writer := NewWriteConstraintWriter(app)

	constraint := access.WriteConstraint{
		Resources: []access.ResourceConstraint{{
			Kind:       "tests",
			ResourceID: "7",
			Revision:   "3",
		}},
	}
	entity := testEntity{id: 7, version: 3}

	err = writer.UpdateWithConstraint(context.Background(), entity, constraint)
	require.NoError(t, err)
	require.Equal(t, entity, repo.updated)
	require.Equal(t, "7", repo.updateConstraint.Resources[0].ResourceID)
}

func TestApplication_CreateWithConstraint_RequiresConstrainedRepository(t *testing.T) {
	app, err := NewApplication[testEntity, int64](&recordingRepo{}, nil, DefaultServiceConfig())
	require.NoError(t, err)
	writer := NewWriteConstraintWriter(app)

	err = writer.CreateWithConstraint(context.Background(), testEntity{id: 1}, access.WriteConstraint{})
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Unsupported))
}

func TestApplication_UpdateAllWithConstraint_MatchesConstraintsByEntityID(t *testing.T) {
	repo := &guardedBatchFallbackRepo{}
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	require.NoError(t, err)
	writer := NewWriteConstraintWriter(app)

	constraint := access.WriteConstraint{
		Resources: []access.ResourceConstraint{
			{Kind: "tests", ResourceID: "2", Revision: "7"},
			{Kind: "tests", ResourceID: "1", Revision: "5"},
		},
	}
	entities := []testEntity{
		{id: 1, version: 5},
		{id: 2, version: 7},
	}

	err = writer.UpdateAllWithConstraint(context.Background(), entities, constraint)
	require.NoError(t, err)
	require.Len(t, repo.updateConstraints, 2)
	require.Equal(t, "1", repo.updateConstraints[0].Resources[0].ResourceID)
	require.Equal(t, "5", repo.updateConstraints[0].Resources[0].Revision)
	require.Equal(t, "2", repo.updateConstraints[1].Resources[0].ResourceID)
	require.Equal(t, "7", repo.updateConstraints[1].Resources[0].Revision)
}

func TestApplication_UpdateAllWithConstraint_RunsBeforeAndValidateOncePerEntity(t *testing.T) {
	repo := &guardedBatchFallbackRepo{}
	validator := &trackingValidator{}
	app, err := NewApplication[testEntity, int64](repo, validator, DefaultServiceConfig())
	require.NoError(t, err)

	var beforeCalls int
	app.SetHooks(&Hooks[testEntity, int64]{
		BeforeUpdate: func(ctx context.Context, entity testEntity) error {
			beforeCalls++
			return nil
		},
	})
	writer := NewWriteConstraintWriter(app)

	constraint := access.WriteConstraint{
		Resources: []access.ResourceConstraint{
			{Kind: "tests", ResourceID: "1", Revision: "5"},
			{Kind: "tests", ResourceID: "2", Revision: "7"},
		},
	}
	entities := []testEntity{
		{id: 1, version: 5},
		{id: 2, version: 7},
	}

	err = writer.UpdateAllWithConstraint(context.Background(), entities, constraint)
	require.NoError(t, err)
	require.Equal(t, 2, beforeCalls)
	require.Equal(t, 2, validator.called)
}
