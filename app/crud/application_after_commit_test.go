package crud

import (
	"context"
	"testing"

	"gochen/contextx"
	"gochen/domain/spec"
	"gochen/errors"
)

func TestCommandResultsSurvivePostCommitFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		create bool
		bulk   bool
	}{
		{name: "ExecuteNew", create: true},
		{name: "Execute"},
		{name: "ExecuteNewBulk", create: true, bulk: true},
		{name: "ExecuteBulk", bulk: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			want := errors.New("post commit failed")
			calls := 0
			stage := Stage[*testEntity]{PostCommit: func(context.Context, *testEntity) error {
				calls++
				return want
			}}
			app, repo := newTestApplication(t, Options[*testEntity, int64]{DeleteMode: DeletePurge,
				Hooks: Hooks[*testEntity]{ActionCreate: stage, ActionUpdate: stage},
			})
			entities := []*testEntity{{Name: "first"}}
			if test.bulk {
				entities = append(entities, &testEntity{Name: "second"})
			}
			ids := make([]int64, 0, len(entities))
			if !test.create {
				for _, entity := range entities {
					if err := repo.Create(ctx, entity, spec.All()); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, entity.GetID())
				}
			}
			update := func(_ context.Context, entity *testEntity) error {
				entity.Name += "-updated"
				return nil
			}
			var results []*testEntity
			var err error
			switch {
			case test.create && test.bulk:
				results, err = app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*testEntity, error) {
					return entities, nil
				})
			case test.create:
				var result *testEntity
				result, err = app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testEntity, error) {
					return entities[0], nil
				})
				results = []*testEntity{result}
			case test.bulk:
				results, err = app.ExecuteBulk(ctx, ActionUpdate, ids, update)
			default:
				var result *testEntity
				result, err = app.Execute(ctx, ActionUpdate, ids[0], update)
				results = []*testEntity{result}
			}
			if !contextx.IsAfterCommitError(err) || !errors.Is(err, want) {
				t.Fatalf("command error = %v, want AfterCommitError wrapping callback failure", err)
			}
			if calls != len(entities) || len(results) != len(entities) {
				t.Fatalf("callbacks=%d results=%d, want %d each", calls, len(results), len(entities))
			}
			for i, result := range results {
				if result == nil || result.GetID() != entities[i].GetID() {
					t.Fatalf("result %d lost the committed entity: %+v", i, result)
				}
				stored, err := app.Get(ctx, result.GetID())
				if err != nil || stored.Name != result.Name || stored.GetVersion() != result.GetVersion() {
					t.Fatalf("result does not match committed state: result=%+v stored=%+v error=%v", result, stored, err)
				}
			}
		})
	}
}
