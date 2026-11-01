package eventsourced

import (
	"context"
	"testing"

	esstore "gochen/app/eventsourced/eventstore"
	"gochen/auth"
	"gochen/contextx"
	deventsourced "gochen/domain/eventsourced"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/eventing/store"
	"gochen/messaging"
	"gochen/messaging/command"
	"gochen/testkit/require"
	"gochen/validate"
)

func newApplicationRepository(t *testing.T) (*EventSourcedRepository[*testAggregate, int64], *store.MemoryEventStore[int64]) {
	t.Helper()
	events := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("ValueSet", func() any { return &valueSetEvent{} }))
	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode:      esstore.PublishModePersistOnly,
		AggregateType:    "TestAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       events,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)
	repo, err := newTestEventSourcedRepository[*testAggregate, int64]("TestAggregate", &testAggregate{}, AdaptAggregateFactory(newTestAggregate), adapter)
	require.NoError(t, err)
	return repo, events
}

func TestApplicationExecuteWritesAuditMetadataAndAuditTrail(t *testing.T) {
	ctx := context.Background()
	repo, events := newApplicationRepository(t)
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource:   "test.aggregate",
		Authorizer: auth.Allow(),
		History:    events,
	})
	require.NoError(t, err)

	ctx, err = contextx.WithOperator(ctx, "alice")
	require.NoError(t, err)
	ctx, err = contextx.WithRequestID(ctx, "req-1")
	require.NoError(t, err)

	created, err := app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testAggregate, error) {
		aggregate := newTestAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&valueSetEvent{V: 1})
	})
	require.NoError(t, err)
	require.Equal(t, 1, created.Value)

	updated, err := app.Execute(ctx, ActionUpdate, 1, func(_ context.Context, aggregate *testAggregate) error {
		return aggregate.ApplyAndRecord(&valueSetEvent{V: 42})
	})
	require.NoError(t, err)
	require.Equal(t, 42, updated.Value)

	stored, err := events.LoadEvents(ctx, "TestAggregate", 1, 0)
	require.NoError(t, err)
	require.Len(t, stored, 2)
	for _, evt := range stored {
		metadata := evt.GetMetadata()
		operator, _ := metadata.GetString(contextx.MetadataOperatorKey)
		correlation, _ := metadata.GetString(contextx.MetadataCorrelationIDKey)
		require.Equal(t, "alice", operator)
		require.Equal(t, "req-1", correlation)
	}
	action, _ := stored[1].GetMetadata().GetString(contextx.MetadataActionKey)
	require.Equal(t, ActionUpdate, action)

	trail, err := app.AuditTrail(ctx, 1, 0, 10)
	require.NoError(t, err)
	require.Len(t, trail, 2)
	require.Equal(t, "alice", trail[1].ActorID)
	require.Equal(t, ActionUpdate, trail[1].Action)
	require.Equal(t, "req-1", trail[1].CorrelationID)
	require.Equal(t, 42, trail[1].RawPayload.(*valueSetEvent).V)
}

type specAuthorizer struct {
	vocabulary auth.Vocabulary
	decision   auth.Decision
}

func (a *specAuthorizer) Authorize(context.Context, auth.Request) (auth.Decision, error) {
	return a.decision, nil
}

func (a *specAuthorizer) Vocabulary() auth.Vocabulary { return a.vocabulary }

func TestApplicationGetReturnsNotFoundOutsideSpec(t *testing.T) {
	ctx := context.Background()
	repo, _ := newApplicationRepository(t)
	visible := newTestAggregate(2)
	require.NoError(t, visible.ApplyAndRecord(&valueSetEvent{V: 5}))
	require.NoError(t, repo.Save(ctx, visible))
	hidden := newTestAggregate(1)
	require.NoError(t, hidden.ApplyAndRecord(&valueSetEvent{V: 42}))
	require.NoError(t, repo.Save(ctx, hidden))

	authorizer := &specAuthorizer{
		vocabulary: auth.Vocabulary{Fields: []string{"Value"}},
		decision:   auth.Decision{Allowed: true, Spec: spec.Eq("Value", 5)},
	}
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource:   "test.aggregate",
		Authorizer: authorizer,
	})
	require.NoError(t, err)

	got, err := app.Get(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, 5, got.Value)

	_, err = app.Get(ctx, 1)
	require.Truef(t, errors.Is(err, errors.NotFound), "mismatched aggregate must be hidden: %v", err)
}

func TestApplicationExecuteRejectsStateOutsideSpec(t *testing.T) {
	ctx := t.Context()
	repo, events := newApplicationRepository(t)
	initial := newTestAggregate(1)
	require.NoError(t, initial.ApplyAndRecord(&valueSetEvent{V: 5}))
	require.NoError(t, repo.Save(ctx, initial))
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource: "test.aggregate",
		Authorizer: &specAuthorizer{
			vocabulary: auth.Vocabulary{Fields: []string{"Value"}},
			decision:   auth.Decision{Allowed: true, Spec: spec.Eq("Value", 5)},
		},
	})
	require.NoError(t, err)

	result, err := app.Execute(ctx, ActionUpdate, 1, func(_ context.Context, aggregate *testAggregate) error {
		return aggregate.ApplyAndRecord(&valueSetEvent{V: 42})
	})
	require.Truef(t, errors.Is(err, errors.Forbidden), "command result outside spec must be rejected: %v", err)
	require.True(t, result == nil)
	stored, err := repo.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 5, stored.Value)
	stream, err := events.LoadEvents(ctx, "TestAggregate", 1, 0)
	require.NoError(t, err)
	require.Len(t, stream, 1)
}

func TestApplicationExecuteNewRejectsExistingAggregate(t *testing.T) {
	ctx := t.Context()
	repo, events := newApplicationRepository(t)
	initial := newTestAggregate(1)
	require.NoError(t, initial.ApplyAndRecord(&valueSetEvent{V: 5}))
	require.NoError(t, repo.Save(ctx, initial))
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource: "test.aggregate", Authorizer: auth.Allow(),
	})
	require.NoError(t, err)

	result, err := app.ExecuteNew(ctx, ActionCreate, func(ctx context.Context) (*testAggregate, error) {
		aggregate, err := repo.Get(ctx, 1)
		if err != nil {
			return nil, err
		}
		return aggregate, aggregate.ApplyAndRecord(&valueSetEvent{V: 42})
	})
	require.Truef(t, errors.Is(err, errors.InvalidInput), "create requires a new aggregate: %v", err)
	require.True(t, result == nil)
	stored, err := repo.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 5, stored.Value)
	stream, err := events.LoadEvents(ctx, "TestAggregate", 1, 0)
	require.NoError(t, err)
	require.Len(t, stream, 1)
}

func TestApplicationExecuteNewValidatesAuthoritativeAssignment(t *testing.T) {
	ctx := context.Background()
	events := store.NewMemoryEventStore[int64]()
	reg, upgraders := newTestRegistryAndUpgraders()
	require.NoError(t, reg.Register("OwnedValueSet", func() any { return &ownedValueEvent{} }))
	adapter, err := esstore.NewDomainEventStore(esstore.DomainEventStoreOptions[int64]{
		PublishMode:      esstore.PublishModePersistOnly,
		AggregateType:    "OwnedAggregate",
		EventIDGenerator: testEventIDGenerator(),
		EventStore:       events,
		EventRegistry:    reg,
		UpgraderRegistry: upgraders,
	})
	require.NoError(t, err)
	repo, err := NewEventSourcedRepository(RepositoryOptions[*ownedAggregate, int64]{
		AggregateType:    "OwnedAggregate",
		Sample:           &ownedAggregate{},
		Factory:          AdaptAggregateFactory(newOwnedAggregate),
		Store:            adapter,
		MetadataRegistry: ownedMetadataRegistry,
	})
	require.NoError(t, err)
	authorizer := &specAuthorizer{
		vocabulary: auth.Vocabulary{Roles: []spec.Role{spec.RoleOwner}},
		decision: auth.Decision{
			Allowed: true,
			Spec:    spec.All(),
			Assign:  []auth.Assignment{{Role: spec.RoleOwner, Value: "alice"}},
		},
	}
	app, err := NewApplication[*ownedAggregate, int64](repo, Options[*ownedAggregate, int64]{
		Resource:   "owned.aggregate",
		Authorizer: authorizer,
	})
	require.NoError(t, err)

	_, err = app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*ownedAggregate, error) {
		aggregate := newOwnedAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&ownedValueEvent{V: 1, Owner: "bob"})
	})
	require.Truef(t, errors.Is(err, errors.Forbidden), "assignment mismatch must reject: %v", err)

	created, err := app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*ownedAggregate, error) {
		aggregate := newOwnedAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&ownedValueEvent{V: 1, Owner: "alice"})
	})
	require.NoError(t, err)
	require.Equal(t, "alice", created.Owner)
}

type flakyRepository struct {
	deventsourced.IEventSourcedRepository[*testAggregate, int64]
	failures int
	saves    int
}

func (r *flakyRepository) Save(ctx context.Context, aggregate *testAggregate) error {
	r.saves++
	if r.saves <= r.failures {
		return errors.NewCode(errors.Concurrency, "concurrent modification")
	}
	return r.IEventSourcedRepository.Save(ctx, aggregate)
}

func TestApplicationRetriesConcurrencyConflicts(t *testing.T) {
	ctx := context.Background()
	repo, _ := newApplicationRepository(t)
	flaky := &flakyRepository{IEventSourcedRepository: repo}
	app, err := NewApplication[*testAggregate, int64](flaky, Options[*testAggregate, int64]{
		Resource:         "test.aggregate",
		Authorizer:       auth.Allow(),
		ConcurrencyRetry: &RetryConfig{MaxRetries: 3},
	})
	require.NoError(t, err)

	created, err := app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testAggregate, error) {
		aggregate := newTestAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&valueSetEvent{V: 1})
	})
	require.NoError(t, err)
	require.Equal(t, 1, created.Value)

	flaky.failures = flaky.saves + 2
	calls := 0
	updated, err := app.Execute(ctx, ActionUpdate, 1, func(_ context.Context, aggregate *testAggregate) error {
		calls++
		return aggregate.ApplyAndRecord(&valueSetEvent{V: 42})
	})
	require.NoError(t, err)
	require.Equal(t, 42, updated.Value)
	require.Equal(t, 3, calls)
}

func TestApplicationExecuteNewDoesNotRetry(t *testing.T) {
	ctx := context.Background()
	repo, _ := newApplicationRepository(t)
	flaky := &flakyRepository{IEventSourcedRepository: repo, failures: 5}
	app, err := NewApplication[*testAggregate, int64](flaky, Options[*testAggregate, int64]{
		Resource:         "test.aggregate",
		Authorizer:       auth.Allow(),
		ConcurrencyRetry: &RetryConfig{MaxRetries: 3},
	})
	require.NoError(t, err)

	_, err = app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testAggregate, error) {
		aggregate := newTestAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&valueSetEvent{V: 1})
	})
	require.Truef(t, errors.Is(err, errors.Concurrency), "create conflict must surface: %v", err)
	require.Equal(t, 1, flaky.saves)
}

type nonNegativeValidator struct{}

func (nonNegativeValidator) Validate(value any) error {
	aggregate, ok := value.(*testAggregate)
	if !ok {
		return nil
	}
	if aggregate.Value < 0 {
		return validate.NewError("value must be non-negative")
	}
	return nil
}

func TestApplicationValidatesAfterCommand(t *testing.T) {
	ctx := context.Background()
	repo, events := newApplicationRepository(t)
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource:   "test.aggregate",
		Authorizer: auth.Allow(),
		Validator:  nonNegativeValidator{},
	})
	require.NoError(t, err)

	_, err = app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testAggregate, error) {
		aggregate := newTestAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&valueSetEvent{V: 5})
	})
	require.NoError(t, err)

	_, err = app.Execute(ctx, ActionUpdate, 1, func(_ context.Context, aggregate *testAggregate) error {
		return aggregate.ApplyAndRecord(&valueSetEvent{V: -1})
	})
	require.Truef(t, errors.Is(err, errors.Validation), "post-command validation must reject: %v", err)

	stored, err := events.LoadEvents(ctx, "TestAggregate", 1, 0)
	require.NoError(t, err)
	require.Len(t, stored, 1)
}

func TestApplicationRejectsNamedVocabulary(t *testing.T) {
	repo, _ := newApplicationRepository(t)
	authorizer := &specAuthorizer{
		vocabulary: auth.Vocabulary{Named: []string{"visibleTo"}},
		decision:   auth.Decision{Allowed: true, Spec: spec.All()},
	}
	_, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource:   "test.aggregate",
		Authorizer: authorizer,
	})
	require.Truef(t, errors.Is(err, errors.InvalidInput), "named conditions must be rejected: %v", err)
}

func TestApplicationAuditTrailRejectsNonBoundarySpec(t *testing.T) {
	ctx := context.Background()
	repo, events := newApplicationRepository(t)
	authorizer := &specAuthorizer{
		vocabulary: auth.Vocabulary{Fields: []string{"Value"}},
		decision:   auth.Decision{Allowed: true, Spec: spec.Eq("Value", 1)},
	}
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource:   "test.aggregate",
		Authorizer: authorizer,
		History:    events,
	})
	require.NoError(t, err)
	_, err = app.AuditTrail(ctx, 1, 0, 10)
	require.Truef(t, errors.Is(err, errors.Internal), "audit spec must only reference boundary roles: %v", err)
}

type setAggregateCommand struct {
	ID int64
	V  int
}

func (c *setAggregateCommand) AggregateID() int64 { return c.ID }

func (c *setAggregateCommand) Apply(_ context.Context, aggregate *testAggregate) error {
	return aggregate.ApplyAndRecord(&valueSetEvent{V: c.V})
}

func TestApplicationCommandMessageHandlerCallsExecute(t *testing.T) {
	ctx := context.Background()
	repo, _ := newApplicationRepository(t)
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource:   "test.aggregate",
		Authorizer: auth.Allow(),
	})
	require.NoError(t, err)

	_, err = app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*testAggregate, error) {
		aggregate := newTestAggregate(1)
		return aggregate, aggregate.ApplyAndRecord(&valueSetEvent{V: 1})
	})
	require.NoError(t, err)

	handler := AsExecuteHandler[*testAggregate, int64]("set-aggregate", "SetAggregate", app, ActionUpdate,
		func(c *command.Command) (ExecuteCommand[*testAggregate, int64], error) {
			return &setAggregateCommand{ID: 1, V: 7}, nil
		})
	message := command.NewCommand("msg-1", "SetAggregate", "1", "TestAggregate", nil)
	require.NoError(t, handler.Handle(ctx, message))

	loaded, err := repo.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 7, loaded.Value)

	// 非匹配命令类型必须忽略。
	other := command.NewCommand("msg-2", "Other", "1", "TestAggregate", nil)
	require.NoError(t, handler.Handle(ctx, other))
	require.Equal(t, "set-aggregate", handler.Type())
}

func TestApplicationCommandHandlerIgnoresNonCommandMessages(t *testing.T) {
	ctx := context.Background()
	repo, _ := newApplicationRepository(t)
	app, err := NewApplication[*testAggregate, int64](repo, Options[*testAggregate, int64]{
		Resource:   "test.aggregate",
		Authorizer: auth.Allow(),
	})
	require.NoError(t, err)
	handler := AsExecuteHandler[*testAggregate, int64]("noop", "", app, ActionUpdate,
		func(*command.Command) (ExecuteCommand[*testAggregate, int64], error) {
			return nil, nil
		})
	message := messaging.NewMessage("e1", messaging.KindEvent, "ValueSet", nil)
	require.NoError(t, handler.Handle(ctx, message))
}

type ownedAggregate struct {
	*deventsourced.EventSourcedAggregate[int64]
	Owner string `role:"owner"`
	Value int
}

func newOwnedAggregate(id int64) *ownedAggregate {
	aggregate := &ownedAggregate{}
	init, err := deventsourced.InitAggregate[int64](ownedMetadataRegistry, aggregate, id, "OwnedAggregate")
	if err != nil {
		panic(err)
	}
	aggregate.EventSourcedAggregate = init
	return aggregate
}

func (a *ownedAggregate) ApplyOwnedValueEvent(evt *ownedValueEvent) {
	a.Owner = evt.Owner
	a.Value = evt.V
}

var ownedMetadataRegistry = func() *deventsourced.MetadataRegistry {
	registry := deventsourced.NewMetadataRegistry()
	if _, err := registry.Register(&ownedAggregate{}, "OwnedAggregate"); err != nil {
		panic(err)
	}
	return registry
}()

type ownedValueEvent struct {
	V     int
	Owner string
}

func (e *ownedValueEvent) EventType() string { return "OwnedValueSet" }
