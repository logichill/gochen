package eventstore_test

import (
	"context"
	appes "gochen/app/eventsourced"
	esstore "gochen/app/eventsourced/eventstore"
	"gochen/contextx"
	"gochen/domain"
	es "gochen/domain/eventsourced"
	"gochen/errors"
	"testing"
)

type balanceState struct{ Values map[string]int }

func balanceModel() es.Model[balanceState, domain.IDomainEvent] {
	return es.Model[balanceState, domain.IDomainEvent]{
		Initial: func() balanceState { return balanceState{Values: map[string]int{"balance": 0}} },
		Clone: func(s balanceState) balanceState {
			v := map[string]int{}
			for k, n := range s.Values {
				v[k] = n
			}
			return balanceState{Values: v}
		},
		Reduce: func(s balanceState, event domain.IDomainEvent) (balanceState, error) {
			switch e := event.(type) {
			case *valueSetEvent:
				s.Values["balance"] = e.V
				return s, nil
			default:
				s.Values["balance"] = -1
				return s, errors.NewCode(errors.InvalidInput, "unknown balance event")
			}
		},
	}
}
func TestExplicitStateSameStreamAndFailureIsolation(t *testing.T) {
	ctx := context.Background()
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	stream, err := esstore.NewEventStream(opts)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := appes.NewStateRepository(balanceModel(), stream)
	if err != nil {
		t.Fatal(err)
	}
	base, err := repo.Load(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	decide := func(_ context.Context, s balanceState) ([]domain.IDomainEvent, error) {
		s.Values["balance"] = 999
		return []domain.IDomainEvent{&valueSetEvent{V: 10}, &valueSetEvent{V: 20}}, nil
	}
	committed, err := repo.Execute(ctx, base, decide)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Version() != 2 || committed.Value().Values["balance"] != 20 || base.Value().Values["balance"] != 0 {
		t.Fatal("state or order mismatch")
	}
	view := committed.Value()
	view.Values["balance"] = -10
	if committed.Value().Values["balance"] != 20 {
		t.Fatal("Value leaked mutable state")
	}
	legacy := newTestAggregate(1)
	result, err := stream.RestoreAggregate(ctx, legacy)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.Load(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Value != replayed.Value().Values["balance"] || result.Version != replayed.Version() {
		t.Fatal("legacy and explicit replay differ")
	}
	_, err = repo.Execute(ctx, base, decide)
	if !errors.Is(err, errors.Concurrency) {
		t.Fatalf("stale append: %v", err)
	}
	if base.Value().Values["balance"] != 0 {
		t.Fatal("failed append mutated baseline")
	}
	_, err = repo.Execute(ctx, committed, func(_ context.Context, s balanceState) ([]domain.IDomainEvent, error) {
		s.Values["balance"] = 400
		return nil, errors.NewCode(errors.Validation, "rejected")
	})
	if !errors.Is(err, errors.Validation) || committed.Value().Values["balance"] != 20 {
		t.Fatal("decision failure leaked")
	}
	_, err = repo.Execute(ctx, committed, func(context.Context, balanceState) ([]domain.IDomainEvent, error) {
		return []domain.IDomainEvent{unknownBalanceEvent{}}, nil
	})
	if !errors.Is(err, errors.InvalidInput) || committed.Value().Values["balance"] != 20 {
		t.Fatal("unknown event did not fail safely")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.Load(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("load cancel: %v", err)
	}
	if _, err := repo.Execute(canceled, committed, decide); !errors.Is(err, context.Canceled) {
		t.Fatalf("execute cancel: %v", err)
	}
	mid, cancelMid := context.WithCancel(ctx)
	if _, err := repo.Execute(mid, committed, func(context.Context, balanceState) ([]domain.IDomainEvent, error) {
		cancelMid()
		return []domain.IDomainEvent{&valueSetEvent{V: 99}}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("decision cancel: %v", err)
	}
	version, err := stream.GetAggregateVersion(ctx, 1)
	if err != nil || version != 2 {
		t.Fatalf("failed operations appended: %d %v", version, err)
	}
}

type unknownBalanceEvent struct{}

func (unknownBalanceEvent) EventType() string { return "UnknownBalance" }
func TestExplicitStateAfterCommit(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModeDirect
	opts.EventBus = &modeEventBus{err: errors.NewCode(errors.Dependency, "publish failed")}
	stream, err := esstore.NewEventStream(opts)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := appes.NewStateRepository(balanceModel(), stream)
	if err != nil {
		t.Fatal(err)
	}
	base, err := repo.Load(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	next, err := repo.Execute(context.Background(), base, func(context.Context, balanceState) ([]domain.IDomainEvent, error) {
		return []domain.IDomainEvent{&valueSetEvent{V: 9}}, nil
	})
	if !contextx.IsAfterCommitError(err) || next == nil || next.Version() != 1 || next.Value().Values["balance"] != 9 {
		t.Fatalf("committed result lost: %v", err)
	}
	loaded, err := repo.Load(context.Background(), 1)
	if err != nil || loaded.Version() != 1 {
		t.Fatalf("commit missing: %v", err)
	}
}
func TestExplicitStateUnknownReplay(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	if err := opts.EventRegistry.Register("UnknownBalance", func() any { return &unknownBalanceEvent{} }); err != nil {
		t.Fatal(err)
	}
	stream, err := esstore.NewEventStream(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.AppendEvents(context.Background(), 1, []domain.IDomainEvent{&unknownBalanceEvent{}}, 0); err != nil {
		t.Fatal(err)
	}
	repo, err := appes.NewStateRepository(balanceModel(), stream)
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.Load(context.Background(), 1)
	if !errors.Is(err, errors.InvalidInput) || state != nil {
		t.Fatalf("unknown replay accepted: %v", err)
	}
}

func TestExplicitStateRejectsNilEventsAndForeignBaseline(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	stream, err := esstore.NewEventStream(opts)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := appes.NewStateRepository(balanceModel(), stream)
	if err != nil {
		t.Fatal(err)
	}
	other, err := appes.NewStateRepository(balanceModel(), stream)
	if err != nil {
		t.Fatal(err)
	}
	base, err := repo.Load(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	nilEvent := func(context.Context, balanceState) ([]domain.IDomainEvent, error) {
		var e *valueSetEvent
		return []domain.IDomainEvent{e}, nil
	}
	if _, err := repo.Execute(context.Background(), base, nilEvent); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("nil event: %v", err)
	}
	if _, err := other.Execute(context.Background(), base, nilEvent); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("foreign baseline: %v", err)
	}
	if base.Version() != 0 || base.Value().Values["balance"] != 0 {
		t.Fatal("invalid command mutated baseline")
	}
}

func TestExplicitStreamRejectsAggregateSnapshotsAndBrokenVersions(t *testing.T) {
	opts := modeOptions(t)
	opts.PublishMode = esstore.PublishModePersistOnly
	opts.SnapshotManager = &failingSnapshotLoader{}
	if _, err := esstore.NewEventStream(opts); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("legacy snapshot accepted: %v", err)
	}
	opts.SnapshotManager = nil
	opts.EventStore = &stallingAggregateStreamStore{batched: true}
	stream, err := esstore.NewEventStream(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.ReadEvents(context.Background(), 13, func(domain.IDomainEvent) error { return nil }); !errors.Is(err, errors.Internal) {
		t.Fatalf("repeated version accepted: %v", err)
	}
}
