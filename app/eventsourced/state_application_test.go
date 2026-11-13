package eventsourced

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"gochen/app"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain"
	es "gochen/domain/eventsourced"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
)

type guardedState struct {
	Tenant     string
	Department int64
	Balance    int
}
type guardedView struct {
	ID         int64  `entity:"id"`
	Tenant     string `dim:"tenant"`
	Department int64  `dim:"department"`
	Balance    int
}
type guardedEvent struct {
	Tenant     string
	Department int64
	Balance    int
}

func (guardedEvent) EventType() string { return "guarded.changed" }

type guardedStream struct {
	mu             sync.Mutex
	events         map[int64][]domain.IDomainEvent
	reads, appends int
	appendError    error
	beforeAppend   func()
}

func (s *guardedStream) ReadEvents(_ context.Context, id int64, apply func(domain.IDomainEvent) error) (uint64, error) {
	s.mu.Lock()
	s.reads++
	events := append([]domain.IDomainEvent(nil), s.events[id]...)
	s.mu.Unlock()
	for _, e := range events {
		if err := apply(e); err != nil {
			return 0, err
		}
	}
	return uint64(len(events)), nil
}
func (s *guardedStream) AppendEvents(_ context.Context, id int64, events []domain.IDomainEvent, version uint64) error {
	if s.beforeAppend != nil {
		s.beforeAppend()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if uint64(len(s.events[id])) != version {
		return errors.NewCode(errors.Concurrency, "stale append")
	}
	if s.appendError != nil && !contextx.IsAfterCommitError(s.appendError) {
		return s.appendError
	}
	s.events[id] = append(s.events[id], events...)
	s.appends++
	return s.appendError
}

type guardedAuthorizer struct {
	decision   auth.Decision
	vocabulary auth.Vocabulary
	failure    error
	request    auth.Request
	calls      int
}

func (a *guardedAuthorizer) Authorize(ctx context.Context, r auth.Request) (auth.Decision, error) {
	a.calls++
	a.request = r
	if _, err := auth.RequirePrincipal(ctx); err != nil {
		return auth.Decision{}, err
	}
	if a.failure != nil {
		return auth.Decision{}, a.failure
	}
	return a.decision, nil
}
func (a *guardedAuthorizer) Vocabulary() auth.Vocabulary { return a.vocabulary }
func guardedModel() es.Model[guardedState, guardedEvent] {
	return es.Model[guardedState, guardedEvent]{Initial: func() guardedState { return guardedState{} }, Clone: func(s guardedState) guardedState { return s }, Reduce: func(_ guardedState, e guardedEvent) (guardedState, error) { return guardedState(e), nil }}
}
func guardedOptions(authorizer auth.IAuthorizer) StateOptions[guardedState, int64, *guardedView] {
	return StateOptions[guardedState, int64, *guardedView]{Resource: "account", Authorizer: authorizer, Actions: []auth.Action{{Name: "read", Kind: auth.KindRead}, {Name: "open", Kind: auth.KindCreate}, {Name: "change", Kind: auth.KindUpdate}}, View: func(id int64, s guardedState) *guardedView {
		return &guardedView{ID: id, Tenant: s.Tenant, Department: s.Department, Balance: s.Balance}
	}, Contract: app.ResourceContract{Isolation: meta.ByDimension("tenant")}, Isolation: func(ctx context.Context) (any, error) {
		value := contextx.TenantID(ctx)
		if value == "" {
			return nil, errors.NewCode(errors.Unauthorized, "tenant required")
		}
		return value, nil
	}}
}
func guardedFixture(t *testing.T, seed bool) (context.Context, *StateApplication[guardedState, guardedEvent, int64], *guardedStream, *guardedAuthorizer) {
	t.Helper()
	ctx, err := auth.WithPrincipal(context.Background(), auth.Principal{SubjectID: "alice", TenantID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = contextx.WithTenantID(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	stream := &guardedStream{events: map[int64][]domain.IDomainEvent{}}
	if seed {
		stream.events[1] = []domain.IDomainEvent{guardedEvent{"t1", 10, 5}}
	}
	repo, err := NewStateRepository(guardedModel(), stream)
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &guardedAuthorizer{decision: auth.Decision{Allowed: true, Spec: spec.Eq(meta.ByDimension("department"), int64(10))}, vocabulary: auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("department")}}}
	application, err := NewStateApplication(repo, guardedOptions(authorizer))
	if err != nil {
		t.Fatal(err)
	}
	return ctx, application, stream, authorizer
}
func decideState(e guardedEvent) func(context.Context, guardedState) ([]guardedEvent, error) {
	return func(context.Context, guardedState) ([]guardedEvent, error) { return []guardedEvent{e}, nil }
}

func TestStateApplicationAuthorizationBeforeReadAndBusiness(t *testing.T) {
	ctx, a, stream, az := guardedFixture(t, true)
	called := false
	decide := func(context.Context, guardedState) ([]guardedEvent, error) { called = true; return nil, nil }
	az.decision.Allowed = false
	if _, err := a.Execute(ctx, "change", 1, 1, decide); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("denial: %v", err)
	}
	if stream.reads != 0 || called || stream.appends != 0 {
		t.Fatal("denial reached storage or business")
	}
	az.decision.Allowed = true
	if _, err := a.Execute(ctx, "read", 1, 1, decide); err == nil {
		t.Fatal("read action accepted for write")
	}
	if _, err := a.Load(ctx, "change", 1); err == nil {
		t.Fatal("write action accepted for read")
	}
	missing, _ := contextx.WithTenantID(context.Background(), "t1")
	if _, err := a.Load(missing, "read", 1); !errors.Is(err, errors.Unauthorized) {
		t.Fatalf("missing identity: %v", err)
	}
	if stream.reads != 0 {
		t.Fatal("unauthorized load reached stream")
	}
	loaded, err := a.Load(ctx, "read", 1)
	if err != nil || loaded.Value().Balance != 5 {
		t.Fatalf("valid read: %v", err)
	}
	if az.request.Resource != "account" || az.request.Action != "read" || !reflect.DeepEqual(az.request.IDs, []string{"1"}) {
		t.Fatalf("unbound request: %+v", az.request)
	}
}

func TestStateApplicationChecksBaselineAndFinalStateBeforeAppend(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event guardedEvent
	}{
		{"tenant", guardedEvent{"t2", 10, 9}}, {"department", guardedEvent{"t1", 20, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, a, stream, _ := guardedFixture(t, true)
			_, err := a.Execute(ctx, "change", 1, 1, decideState(tc.event))
			if !errors.Is(err, errors.Forbidden) {
				t.Fatalf("protected result accepted: %v", err)
			}
			if stream.appends != 0 || len(stream.events[1]) != 1 {
				t.Fatal("rejected state appended")
			}
		})
	}
	ctx, a, stream, az := guardedFixture(t, true)
	az.decision.Spec = spec.Eq(meta.ByField("Balance"), 5)
	az.vocabulary = auth.Vocabulary{Refs: []meta.Ref{meta.ByField("Balance")}}
	// Vocabulary is frozen at construction.
	var err error
	a, err = NewStateApplication(a.repo, guardedOptions(az))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Execute(ctx, "change", 1, 1, decideState(guardedEvent{"t1", 10, 7}))
	if !errors.Is(err, errors.Forbidden) || stream.appends != 0 {
		t.Fatalf("final scope: %v", err)
	}
	other, _ := contextx.WithTenantID(ctx, "t2")
	called := false
	_, err = a.Execute(other, "change", 1, 999, func(context.Context, guardedState) ([]guardedEvent, error) { called = true; return nil, nil })
	if !errors.Is(err, errors.NotFound) || called {
		t.Fatalf("cross tenant baseline exposed: %v", err)
	}
	az.decision.Spec = spec.Spec{}
	if _, err := a.Load(ctx, "read", 1); !errors.Is(err, errors.NotFound) {
		t.Fatalf("outside read scope: %v", err)
	}
}

func TestStateApplicationCreateRequiresAssignmentCapability(t *testing.T) {
	for _, tc := range []struct {
		name       string
		assignable bool
		assign     bool
	}{
		{name: "transfer only assignment", assign: true},
		{name: "transfer only without assignment"},
		{name: "assignable assignment", assignable: true, assign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, original, stream, az := guardedFixture(t, false)
			az.vocabulary.Assignable = []meta.Dimension{"department"}
			if tc.assign {
				az.decision.Assign = []auth.Assignment{{Dimension: "department", Value: int64(10)}}
			}
			options := guardedOptions(az)
			options.Contract.Transferable = []meta.Dimension{"department"}
			if tc.assignable {
				options.Contract.Assignable = []meta.Dimension{"department"}
			}
			a, err := NewStateApplication(original.repo, options)
			if err != nil {
				t.Fatal(err)
			}
			if a.execution.CanAssign("department") != tc.assignable || !a.execution.CanTransfer("department") {
				t.Fatal("unexpected resource capabilities")
			}
			calls := 0
			result, err := a.Execute(ctx, "open", 1, 0, func(context.Context, guardedState) ([]guardedEvent, error) {
				calls++
				return []guardedEvent{{"t1", 10, 3}}, nil
			})
			if az.calls != 1 || az.request.Action != "open" {
				t.Fatalf("create did not authorize once: calls=%d request=%+v", az.calls, az.request)
			}
			if tc.assign && !tc.assignable {
				if !errors.Is(err, errors.Internal) || result != nil {
					t.Errorf("create assignment without capability: result=%+v err=%v", result, err)
				}
				if stream.reads != 0 || calls != 0 || stream.appends != 0 || len(stream.events[1]) != 0 {
					t.Errorf("invalid assignment reached load/decide/append: reads=%d decides=%d appends=%d events=%d", stream.reads, calls, stream.appends, len(stream.events[1]))
				}
				return
			}
			if err != nil || result == nil || result.Version() != 1 || result.Value().Department != 10 {
				t.Fatalf("valid create: result=%+v err=%v", result, err)
			}
			if stream.reads != 1 || calls != 1 || stream.appends != 1 || len(stream.events[1]) != 1 {
				t.Fatalf("valid create execution: reads=%d decides=%d appends=%d events=%d", stream.reads, calls, stream.appends, len(stream.events[1]))
			}
		})
	}
}

func TestStateApplicationCreateAssignmentsAreProducedByEvents(t *testing.T) {
	ctx, a, stream, az := guardedFixture(t, false)
	az.decision.Assign = []auth.Assignment{{Dimension: "department", Value: int64(10)}}
	az.vocabulary.Assignable = []meta.Dimension{"department"}
	options := guardedOptions(az)
	options.Contract.Assignable = []meta.Dimension{"department"}
	var err error
	a, err = NewStateApplication(a.repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Execute(ctx, "open", 1, 0, decideState(guardedEvent{"t1", 20, 3})); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("assignment mismatch: %v", err)
	}
	if stream.appends != 0 {
		t.Fatal("mismatched create appended")
	}
	result, err := a.Execute(ctx, "open", 1, 0, decideState(guardedEvent{"t1", 10, 3}))
	if err != nil || result.Value().Department != 10 || result.Version() != 1 {
		t.Fatalf("valid create: %v", err)
	}
	if _, err := a.Execute(ctx, "open", 1, 0, decideState(guardedEvent{"t1", 10, 3})); !errors.Is(err, errors.Concurrency) {
		t.Fatalf("recreate: %v", err)
	}
	if _, err := a.Execute(ctx, "change", 2, 0, decideState(guardedEvent{"t1", 10, 3})); !errors.Is(err, errors.NotFound) {
		t.Fatalf("update absent: %v", err)
	}
}

func TestStateApplicationUpdateProtectsTransferOnlyAssignments(t *testing.T) {
	for _, tc := range []struct {
		name       string
		department int64
	}{
		{name: "unchanged", department: 10},
		{name: "changed", department: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, original, stream, az := guardedFixture(t, true)
			az.decision.Spec = spec.All()
			az.decision.Assign = []auth.Assignment{{Dimension: "department", Value: tc.department}}
			az.vocabulary.Assignable = []meta.Dimension{"department"}
			options := guardedOptions(az)
			options.Contract.Transferable = []meta.Dimension{"department"}
			a, err := NewStateApplication(original.repo, options)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			result, err := a.Execute(ctx, "change", 1, 1, func(context.Context, guardedState) ([]guardedEvent, error) {
				calls++
				return []guardedEvent{{"t1", tc.department, 6}}, nil
			})
			if az.calls != 1 || stream.reads != 1 || calls != 1 {
				t.Fatalf("update did not reach decision: authorizations=%d reads=%d decides=%d", az.calls, stream.reads, calls)
			}
			if tc.department != 10 {
				if !errors.Is(err, errors.Forbidden) || result != nil || stream.appends != 0 || len(stream.events[1]) != 1 {
					t.Fatalf("protected assignment changed state: result=%+v err=%v appends=%d events=%d", result, err, stream.appends, len(stream.events[1]))
				}
				return
			}
			if err != nil || result == nil || result.Version() != 2 || result.Value().Department != 10 || result.Value().Balance != 6 || stream.appends != 1 || len(stream.events[1]) != 2 {
				t.Fatalf("unchanged assignment blocked update: result=%+v err=%v appends=%d events=%d", result, err, stream.appends, len(stream.events[1]))
			}
		})
	}
}

func TestStateApplicationVersionFailuresAndCommittedError(t *testing.T) {
	ctx, a, stream, _ := guardedFixture(t, true)
	calls := 0
	decide := func(context.Context, guardedState) ([]guardedEvent, error) {
		calls++
		return []guardedEvent{{"t1", 10, 7}}, nil
	}
	if _, err := a.Execute(ctx, "change", 1, 0, decide); !errors.Is(err, errors.Concurrency) || calls != 0 {
		t.Fatalf("stale input: %v", err)
	}
	stream.beforeAppend = func() {
		stream.mu.Lock()
		stream.events[1] = append(stream.events[1], guardedEvent{"t1", 10, 6})
		stream.mu.Unlock()
	}
	if _, err := a.Execute(ctx, "change", 1, 1, decide); !errors.Is(err, errors.Concurrency) || calls != 1 || stream.appends != 0 {
		t.Fatalf("append conflict/retry: %v", err)
	}
	stream.beforeAppend = nil
	stream.appendError = contextx.WrapAfterCommitError(errors.NewCode(errors.Dependency, "publication failed"))
	result, err := a.Execute(ctx, "change", 1, 2, decide)
	if !contextx.IsAfterCommitError(err) || result == nil || result.Version() != 3 || stream.appends != 1 || calls != 2 {
		t.Fatalf("committed outcome lost: %+v %v", result, err)
	}
}

func TestStateApplicationFinalValidationRunsForNoEventsAndUsesReplay(t *testing.T) {
	ctx, a, stream, az := guardedFixture(t, true)
	validations := 0
	options := guardedOptions(az)
	options.Validate = func(s guardedState) error {
		validations++
		if s.Balance == 5 {
			return errors.NewCode(errors.Validation, "replayed balance denied")
		}
		return nil
	}
	var err error
	a, err = NewStateApplication(a.repo, options)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Execute(ctx, "change", 1, 1, func(_ context.Context, s guardedState) ([]guardedEvent, error) { s.Balance = 100; return nil, nil })
	if !errors.Is(err, errors.Validation) || validations != 1 || stream.appends != 0 {
		t.Fatalf("no-event validation: %v", err)
	}
	// The mutation-only callback is irrelevant; emitted events determine the actual result.
	result, err := a.Execute(ctx, "change", 1, 1, func(_ context.Context, s guardedState) ([]guardedEvent, error) {
		s.Tenant = "t2"
		return []guardedEvent{{"t1", 10, 8}}, nil
	})
	if err != nil || result.Value().Tenant != "t1" || result.Value().Balance != 8 {
		t.Fatalf("callback state leaked: %v", err)
	}
}

func TestStateApplicationRejectsInvalidConstructionAndViews(t *testing.T) {
	ctx, a, stream, az := guardedFixture(t, true)
	tests := map[string]func(*StateOptions[guardedState, int64, *guardedView]){
		"no auth":    func(o *StateOptions[guardedState, int64, *guardedView]) { o.Authorizer = nil },
		"no view":    func(o *StateOptions[guardedState, int64, *guardedView]) { o.View = nil },
		"no actions": func(o *StateOptions[guardedState, int64, *guardedView]) { o.Actions = nil },
		"invalid kind": func(o *StateOptions[guardedState, int64, *guardedView]) {
			o.Actions = []auth.Action{{Name: "delete", Kind: auth.KindDelete}}
		},
		"missing isolation": func(o *StateOptions[guardedState, int64, *guardedView]) { o.Isolation = nil },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			options := guardedOptions(az)
			change(&options)
			if _, err := NewStateApplication(a.repo, options); err == nil {
				t.Fatal("invalid options accepted")
			}
		})
	}
	for _, view := range []func(int64, guardedState) *guardedView{
		func(int64, guardedState) *guardedView { return nil },
		func(_ int64, s guardedState) *guardedView {
			return &guardedView{ID: 99, Tenant: s.Tenant, Department: s.Department}
		},
	} {
		options := guardedOptions(az)
		options.View = view
		bad, err := NewStateApplication(a.repo, options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bad.Execute(ctx, "change", 1, 1, decideState(guardedEvent{"t1", 10, 7})); err == nil {
			t.Fatal("invalid view accepted")
		}
	}
	if stream.appends != 0 {
		t.Fatal("invalid view appended")
	}
	var nilApp *StateApplication[guardedState, guardedEvent, int64]
	if _, err := nilApp.Load(ctx, "read", 1); err == nil {
		t.Fatal("nil app accepted")
	}
	if _, err := a.Load(nil, "read", 1); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := a.Load(ctx, "read", 0); err == nil {
		t.Fatal("zero ID accepted")
	}
	if _, err := a.Execute(ctx, "change", 1, 1, nil); err == nil {
		t.Fatal("nil decide accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.Load(canceled, "read", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	base, err := a.repo.Load(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.repo.execute(ctx, base, decideState(guardedEvent{"t1", 10, 8}), nil); err == nil {
		t.Fatal("nil guard accepted")
	}
}

func TestStateApplicationEmptyScopeAndCancellationNeverAppend(t *testing.T) {
	ctx, a, stream, az := guardedFixture(t, false)
	az.decision.Spec = spec.Spec{}
	called := false
	if _, err := a.Execute(ctx, "open", 1, 0, func(context.Context, guardedState) ([]guardedEvent, error) { called = true; return nil, nil }); !errors.Is(err, errors.Forbidden) || called || stream.reads != 0 {
		t.Fatalf("empty scope ran create: %v", err)
	}
	ctx, a, stream, az = guardedFixture(t, true)
	canceled, cancel := context.WithCancel(ctx)
	if _, err := a.Execute(canceled, "change", 1, 1, func(context.Context, guardedState) ([]guardedEvent, error) {
		cancel()
		return []guardedEvent{{"t1", 10, 7}}, nil
	}); !errors.Is(err, context.Canceled) || stream.appends != 0 {
		t.Fatalf("canceled decision appended: %v", err)
	}
	options := guardedOptions(az)
	canceled, cancel = context.WithCancel(ctx)
	options.Validate = func(guardedState) error { cancel(); return nil }
	a, err := NewStateApplication(a.repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Execute(canceled, "change", 1, 1, decideState(guardedEvent{"t1", 10, 7})); !errors.Is(err, context.Canceled) || stream.appends != 0 {
		t.Fatalf("canceled guard appended: %v", err)
	}
}

type stringGuardedView struct {
	ID string `entity:"id"`
}
type stringGuardedStream struct{ reads int }

func (s *stringGuardedStream) ReadEvents(context.Context, string, func(domain.IDomainEvent) error) (uint64, error) {
	s.reads++
	return 0, nil
}
func (s *stringGuardedStream) AppendEvents(context.Context, string, []domain.IDomainEvent, uint64) error {
	return nil
}
func TestStateApplicationRejectsNoncanonicalStringTargets(t *testing.T) {
	stream := &stringGuardedStream{}
	repo, err := NewStateRepository(guardedModel(), stream)
	if err != nil {
		t.Fatal(err)
	}
	az := &guardedAuthorizer{decision: auth.Decision{Allowed: true, Spec: spec.All()}}
	options := StateOptions[guardedState, string, *stringGuardedView]{Resource: "account", Authorizer: az, Actions: []auth.Action{{Name: "read", Kind: auth.KindRead}}, View: func(id string, _ guardedState) *stringGuardedView { return &stringGuardedView{ID: id} }}
	a, err := NewStateApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", " ", " account", "account\n"} {
		if _, err := a.Load(context.Background(), "read", id); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("noncanonical target %q: %v", id, err)
		}
	}
	if az.calls != 0 || stream.reads != 0 {
		t.Fatal("noncanonical target reached authorization or stream")
	}
}

func TestStateApplicationRejectsUnboundProjectionVocabulary(t *testing.T) {
	_, a, _, az := guardedFixture(t, true)
	az.vocabulary.Refs = []meta.Ref{meta.ByField("Missing")}
	if _, err := NewStateApplication(a.repo, guardedOptions(az)); err == nil {
		t.Fatal("unbound field accepted")
	}
	az.vocabulary.Refs = []meta.Ref{meta.ByDimension("tenant")}
	if _, err := NewStateApplication(a.repo, guardedOptions(az)); err == nil {
		t.Fatal("policy over structural isolation accepted")
	}
	var typedNil *guardedAuthorizer
	if _, err := NewStateApplication(a.repo, guardedOptions(typedNil)); err == nil {
		t.Fatal("typed nil authorizer accepted")
	}
	type wrongIDView struct {
		ID string `entity:"id"`
	}
	if _, err := NewStateApplication(a.repo, StateOptions[guardedState, int64, *wrongIDView]{Resource: "account", Authorizer: az, Actions: []auth.Action{{Name: "read", Kind: auth.KindRead}}, View: func(int64, guardedState) *wrongIDView { return &wrongIDView{} }}); err == nil {
		t.Fatal("wrong ID field accepted")
	}
}

type pointerIsolationView struct {
	ID         int64   `entity:"id"`
	Tenant     *string `dim:"tenant"`
	Department int64   `dim:"department"`
}

func TestStateApplicationNormalizesPointerIsolation(t *testing.T) {
	for _, pointerResolver := range []bool{false, true} {
		t.Run(map[bool]string{false: "scalar resolver", true: "pointer resolver"}[pointerResolver], func(t *testing.T) {
			ctx, original, stream, az := guardedFixture(t, false)
			options := StateOptions[guardedState, int64, *pointerIsolationView]{
				Resource: "account", Authorizer: az, Actions: guardedOptions(az).Actions,
				View: func(id int64, state guardedState) *pointerIsolationView {
					var tenant *string
					if state.Tenant != "" {
						tenant = &state.Tenant
					}
					return &pointerIsolationView{ID: id, Tenant: tenant, Department: state.Department}
				},
				Contract: app.ResourceContract{Isolation: meta.ByDimension("tenant")},
				Isolation: func(ctx context.Context) (any, error) {
					tenant := contextx.TenantID(ctx)
					if pointerResolver {
						return &tenant, nil
					}
					return tenant, nil
				},
			}
			a, err := NewStateApplication(original.repo, options)
			if err != nil {
				t.Fatal(err)
			}
			created, err := a.Execute(ctx, "open", 1, 0, decideState(guardedEvent{"t1", 10, 5}))
			if err != nil || created.Version() != 1 {
				t.Fatalf("matching pointer create: %v", err)
			}
			loaded, err := a.Load(ctx, "read", 1)
			if err != nil || loaded.Value().Tenant != "t1" {
				t.Fatalf("matching pointer read: %v", err)
			}
			updated, err := a.Execute(ctx, "change", 1, 1, decideState(guardedEvent{"t1", 10, 6}))
			if err != nil || updated.Version() != 2 {
				t.Fatalf("matching pointer update: %v", err)
			}
			other, _ := contextx.WithTenantID(ctx, "t2")
			if _, err := a.Load(other, "read", 1); !errors.Is(err, errors.NotFound) {
				t.Fatalf("cross pointer tenant exposed: %v", err)
			}
			for _, tenant := range []string{"t2", ""} {
				if _, err := a.Execute(ctx, "change", 1, 2, decideState(guardedEvent{tenant, 10, 7})); !errors.Is(err, errors.Forbidden) {
					t.Fatalf("changed or NULL isolation accepted: %v", err)
				}
			}
			var missing *string
			options.Isolation = func(context.Context) (any, error) { return &missing, nil }
			noIsolation, err := NewStateApplication(original.repo, options)
			if err != nil {
				t.Fatal(err)
			}
			reads := stream.reads
			if _, err := noIsolation.Load(ctx, "read", 1); !errors.Is(err, errors.Unauthorized) || stream.reads != reads {
				t.Fatalf("nested NULL resolver reached storage: %v", err)
			}
			if stream.appends != 2 {
				t.Fatal("rejected pointer isolation appended")
			}
		})
	}
}

func TestStateApplicationCreateHidesExistingOutOfScopeStreams(t *testing.T) {
	ctx, a, stream, az := guardedFixture(t, true)
	called := false
	decide := func(context.Context, guardedState) ([]guardedEvent, error) {
		called = true
		return []guardedEvent{{"t1", 10, 6}}, nil
	}
	other, _ := contextx.WithTenantID(ctx, "t2")
	if _, err := a.Execute(other, "open", 1, 0, decide); !errors.Is(err, errors.NotFound) || called {
		t.Fatalf("create disclosed cross tenant stream: %v", err)
	}
	az.decision.Spec = spec.Eq(meta.ByDimension("department"), int64(20))
	if _, err := a.Execute(ctx, "open", 1, 0, decide); !errors.Is(err, errors.NotFound) || called {
		t.Fatalf("create disclosed out of scope stream: %v", err)
	}
	az.decision.Spec = spec.Eq(meta.ByDimension("department"), int64(10))
	if _, err := a.Execute(ctx, "open", 1, 0, decide); !errors.Is(err, errors.Concurrency) || called {
		t.Fatalf("same scope existing stream: %v", err)
	}
	if stream.appends != 0 {
		t.Fatal("existing stream create appended")
	}
	created, err := a.Execute(ctx, "open", 2, 0, decide)
	if err != nil || !called || created.Version() != 1 || stream.appends != 1 {
		t.Fatalf("new stream create: %v", err)
	}
}
