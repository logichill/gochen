package eventsourced

import (
	"context"
	"fmt"
	"gochen/app"
	"gochen/auth"
	deventsourced "gochen/domain/eventsourced"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit/require"
	"testing"
)

func TestExecuteNewRequiresAssignmentCapability(t *testing.T) {
	for _, assignable := range []bool{false, true} {
		t.Run(fmt.Sprintf("assignable=%t", assignable), func(t *testing.T) {
			repo, events := newApplicationRepository(t)
			contract := app.ResourceContract{Transferable: []meta.Dimension{"department"}}
			if assignable {
				contract.Assignable = []meta.Dimension{"department"}
			}
			policy := &specAuthorizer{vocabulary: auth.Vocabulary{Assignable: []meta.Dimension{"department"}},
				decision: auth.Decision{Allowed: true, Spec: spec.All(), Assign: []auth.Assignment{{Dimension: "department", Value: int64(2)}}}}
			application, err := NewApplication(repo, Options[*testAggregate, int64]{Resource: "test.aggregate", Authorizer: policy, Contract: contract})
			require.NoError(t, err)
			_, err = application.ExecuteNew(context.Background(), ActionCreate, func(context.Context) (*testAggregate, error) {
				aggregate := newTestAggregate(1)
				value := int64(2)
				return aggregate, aggregate.ApplyAndRecord(&dimensionsSetEvent{Department: &value})
			})
			stored, loadErr := events.LoadEvents(context.Background(), "TestAggregate", 1, 0)
			require.NoError(t, loadErr)
			if assignable {
				require.NoError(t, err)
				require.Len(t, stored, 1)
			} else {
				require.True(t, errors.Is(err, errors.Internal), "assignment without resource capability must fail")
				require.Len(t, stored, 0)
			}
		})
	}
}

func TestExecuteProtectsNullableDimensionsWithoutAssignments(t *testing.T) {
	for _, initial := range []*int64{nil, func() *int64 { value := int64(1); return &value }()} {
		repo, events := newApplicationRepository(t)
		application, err := NewApplication(repo, Options[*testAggregate, int64]{Resource: "test.aggregate", Authorizer: auth.Allow(), Contract: app.ResourceContract{Mutable: []meta.Dimension{"classification"}}})
		require.NoError(t, err)
		_, err = application.ExecuteNew(context.Background(), ActionCreate, func(context.Context) (*testAggregate, error) {
			a := newTestAggregate(1)
			return a, a.ApplyAndRecord(&dimensionsSetEvent{Department: initial})
		})
		require.NoError(t, err)
		_, err = application.Execute(context.Background(), ActionUpdate, 1, func(_ context.Context, a *testAggregate) error {
			value := int64(2)
			return a.ApplyAndRecord(&dimensionsSetEvent{Department: &value})
		})
		require.True(t, errors.Is(err, errors.Forbidden))
		stored, err := events.LoadEvents(context.Background(), "TestAggregate", 1, 0)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		after, err := application.Execute(context.Background(), ActionUpdate, 1, func(_ context.Context, a *testAggregate) error {
			return a.ApplyAndRecord(&dimensionsSetEvent{Department: a.Department, Classification: true})
		})
		require.NoError(t, err)
		require.True(t, after.Classification)
	}
}

type dimensionRetryRepository struct {
	deventsourced.IEventSourcedRepository[*testAggregate, int64]
	conflict bool
}

func (r *dimensionRetryRepository) Save(ctx context.Context, a *testAggregate) error {
	if !r.conflict {
		return r.IEventSourcedRepository.Save(ctx, a)
	}
	r.conflict = false
	current, err := r.Get(ctx, a.GetID())
	if err != nil {
		return err
	}
	value := int64(2)
	if err = current.ApplyAndRecord(&dimensionsSetEvent{Department: &value}); err != nil {
		return err
	}
	if err = r.IEventSourcedRepository.Save(ctx, current); err != nil {
		return err
	}
	return errors.NewCode(errors.Concurrency, "concurrent dimension change")
}

func TestExecuteRetryRecapturesProtectedValues(t *testing.T) {
	repo, _ := newApplicationRepository(t)
	r := &dimensionRetryRepository{IEventSourcedRepository: repo}
	a, err := NewApplication[*testAggregate, int64](r, Options[*testAggregate, int64]{Resource: "test.aggregate", Authorizer: auth.Allow(), Contract: app.ResourceContract{Mutable: []meta.Dimension{"classification"}}, ConcurrencyRetry: &RetryConfig{MaxRetries: 1}})
	require.NoError(t, err)
	_, err = a.ExecuteNew(context.Background(), ActionCreate, func(context.Context) (*testAggregate, error) {
		value := int64(1)
		aggregate := newTestAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&dimensionsSetEvent{Department: &value})
	})
	require.NoError(t, err)
	r.conflict = true
	var seen []int64
	after, err := a.Execute(context.Background(), ActionUpdate, 1, func(_ context.Context, aggregate *testAggregate) error {
		seen = append(seen, *aggregate.Department)
		return aggregate.ApplyAndRecord(&dimensionsSetEvent{Department: aggregate.Department, Classification: true})
	})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, seen)
	require.Equal(t, int64(2), *after.Department)
}
