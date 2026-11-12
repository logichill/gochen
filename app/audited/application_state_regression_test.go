package audited

import (
	"context"
	"testing"

	"gochen/app/crud"
	"gochen/errors"
	"gochen/testkit/require"
)

type maskedAuditValue string

func (maskedAuditValue) MarshalJSON() ([]byte, error) { return []byte(`"masked"`), nil }

func TestAuditedUpdatesPersistChangesHiddenByJSON(t *testing.T) {
	for _, mode := range []string{"update", "execute", "bulk"} {
		for _, projected := range []bool{false, true} {
			opts := Options[*auditDoc, int64]{}
			if projected {
				opts.Projector = func(e *auditDoc) (any, error) { return map[string]any{"name": e.Name}, nil }
			}
			app, repo := newAuditedApplication(t, opts)
			ctx := operatorContext(t, "alice")
			entity := &auditDoc{Name: "doc", Extra: maskedAuditValue("old")}
			require.NoError(t, app.Create(ctx, entity))
			change := func(_ context.Context, candidate *auditDoc) error {
				candidate.Extra = maskedAuditValue("new")
				return nil
			}
			var err error
			switch mode {
			case "update":
				require.NoError(t, change(ctx, entity))
				err = app.Update(ctx, entity)
			case "execute":
				_, err = app.Execute(ctx, crud.ActionUpdate, entity.ID, change)
			case "bulk":
				_, err = app.ExecuteBulk(ctx, crud.ActionUpdate, []int64{entity.ID}, change)
			}
			require.NoError(t, err)
			stored, err := app.Get(ctx, entity.ID)
			require.NoError(t, err)
			require.Equal(t, maskedAuditValue("new"), stored.Extra)
			require.Equal(t, uint64(1), stored.Version)
			require.Len(t, auditRecords(t, repo, ctx, entity.ID), 2)
			stored.Version = 0
			require.True(t, errors.Is(app.Update(ctx, stored), errors.Concurrency))
		}
	}
}

type privateAuditValue struct{ state map[string]string }

func TestUncopyableStateIsRejectedBeforePresentationHooks(t *testing.T) {
	called := false
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{
		Projector: func(e *auditDoc) (any, error) {
			called = true
			e.Extra.(privateAuditValue).state["name"] = "leaked"
			return e.Name, nil
		},
		Options: crud.Options[*auditDoc, int64]{Hooks: crud.Hooks[*auditDoc]{crud.ActionCreate: {
			Before:     func(context.Context, *auditDoc) error { called = true; return nil },
			After:      func(context.Context, *auditDoc) error { called = true; return nil },
			PostCommit: func(context.Context, *auditDoc) error { called = true; return nil },
		}}},
	})
	ctx := operatorContext(t, "alice")
	state := privateAuditValue{state: map[string]string{"name": "before"}}
	entity := &auditDoc{Name: "doc", Extra: state}
	require.True(t, errors.Is(app.Create(ctx, entity), errors.Unsupported))
	require.False(t, called)
	require.Equal(t, "before", state.state["name"])
	count, err := app.Count(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
	require.Len(t, auditRecords(t, repo, ctx, entity.ID), 0)
}

func TestUncopyableUpdateIsRejectedBeforeBusinessHook(t *testing.T) {
	called := false
	app, repo := newAuditedApplication(t, Options[*auditDoc, int64]{
		Options: crud.Options[*auditDoc, int64]{Hooks: crud.Hooks[*auditDoc]{crud.ActionUpdate: {
			Before: func(_ context.Context, e *auditDoc) error {
				called = true
				e.Extra.(privateAuditValue).state["name"] = "leaked"
				return nil
			},
		}}},
	})
	ctx := operatorContext(t, "alice")
	entity := &auditDoc{Name: "doc", Extra: "before"}
	require.NoError(t, app.Create(ctx, entity))
	state := privateAuditValue{state: map[string]string{"name": "before"}}
	entity.Extra = state
	require.True(t, errors.Is(app.Update(ctx, entity), errors.Unsupported))
	require.False(t, called)
	require.Equal(t, "before", state.state["name"])
	stored, err := app.Get(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "before", stored.Extra)
	require.Equal(t, uint64(0), stored.Version)
	require.Len(t, auditRecords(t, repo, ctx, entity.ID), 1)
}
