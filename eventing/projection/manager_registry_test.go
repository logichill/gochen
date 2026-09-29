package projection

import (
	"context"
	stderrors "errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gochen/testkit/assert"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/internal/testutil"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

func TestProjectionManager_UnregisterRetainsFailedUnsubscribeForRetry(t *testing.T) {
	eventBus := &MockEventBus{unsubscribeErr: stderrors.New("unsubscribe failed")}
	manager, err := NewProjectionManager[int64](store.NewMemoryEventStore[int64](), eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterProjection(NewMockProjection("retry", []string{"Event", "OtherEvent"})); err != nil {
		t.Fatal(err)
	}
	if err := manager.UnregisterProjectionWithContext(context.Background(), "retry"); err == nil {
		t.Fatal("expected unsubscribe error")
	}
	if err := manager.RegisterProjection(NewMockProjection("retry", []string{"Event"})); !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected name to remain reserved while cleanup is pending, got %v", err)
	}
	status, err := manager.ProjectionStatus("retry")
	if err != nil || status.Status != "cleanup_pending" || !strings.Contains(status.LastError, "unsubscribe failed") {
		t.Fatalf("failed cleanup status = %+v, error = %v", status, err)
	}
	if err := manager.StopProjection("retry"); err != nil {
		t.Fatal(err)
	}
	if status := manager.ProjectionStatuses()["retry"]; status.Status != "cleanup_pending" {
		t.Fatalf("Stop cleared pending cleanup: %+v", status)
	}
	if err := manager.UnregisterProjectionWithContext(context.Background(), "retry"); err != nil {
		t.Fatalf("retry unregister: %v", err)
	}
	if _, err := manager.ProjectionStatus("retry"); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected NotFound after successful retry, got %v", err)
	}
	if eventBus.unsubscribeCalls != 3 {
		t.Fatalf("unsubscribe calls = %d, want two initial attempts and one retry", eventBus.unsubscribeCalls)
	}
}

func TestProjectionManager_ConcurrentUnregisterDoesNotRepeatCleanup(t *testing.T) {
	eventBus := &duplicateUnsubscribeEventBus{
		MockEventBus: &MockEventBus{},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	manager, err := NewProjectionManager[int64](store.NewMemoryEventStore[int64](), eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterProjection(NewMockProjection("duplicate-cleanup", []string{"Event"})); err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- manager.UnregisterProjection("duplicate-cleanup") }()
	select {
	case <-eventBus.entered:
	case <-time.After(time.Second):
		t.Fatal("first unregister did not enter unsubscribe")
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- manager.UnregisterProjection("duplicate-cleanup") }()
	close(eventBus.release)

	if err := <-firstDone; err != nil {
		t.Fatalf("first unregister failed: %v", err)
	}
	if err := <-secondDone; !errors.Is(err, errors.NotFound) {
		t.Fatalf("second unregister error = %v, want NotFound", err)
	}
	if got := eventBus.unsubscribeCount(); got != 1 {
		t.Fatalf("unsubscribe calls = %d, want 1", got)
	}
}

type duplicateUnsubscribeEventBus struct {
	*MockEventBus
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (b *duplicateUnsubscribeEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	unsub, err := b.MockEventBus.SubscribeEvent(ctx, eventType, handler)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		b.mu.Lock()
		b.calls++
		call := b.calls
		b.mu.Unlock()
		if call == 1 {
			close(b.entered)
			<-b.release
		}
		return unsub(ctx)
	}, nil
}

func (b *duplicateUnsubscribeEventBus) unsubscribeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func TestProjectionManager_ConcurrentRegistrationReservesName(t *testing.T) {
	eventBus := &MockEventBus{}
	manager, err := NewProjectionManager[int64](store.NewMemoryEventStore[int64](), eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}

	const attempts = 24
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- manager.RegisterProjection(NewMockProjection("same-name", []string{"Event"}))
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errors.Conflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent registration error: %v", err)
		}
	}
	if successes != 1 || conflicts != attempts-1 {
		t.Fatalf("registration outcomes = success:%d conflict:%d, want 1/%d", successes, conflicts, attempts-1)
	}
	eventBus.mu.Lock()
	defer eventBus.mu.Unlock()
	if got := len(eventBus.handlers["Event"]); got != 1 {
		t.Fatalf("event handler count = %d, want 1", got)
	}
}

func TestProjectionManager_RejectsTypedNilProjection(t *testing.T) {
	manager, err := NewProjectionManager[int64](store.NewMemoryEventStore[int64](), &MockEventBus{}, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}

	var projection *MockProjection
	if err := manager.RegisterProjection(projection); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("RegisterProjection typed nil error = %v, want InvalidInput", err)
	}
	if err := manager.RegisterProjectionWithContext(context.Background(), projection); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("RegisterProjectionWithContext typed nil error = %v, want InvalidInput", err)
	}
	if _, err := manager.RegisterProjectionAny(context.Background(), projection); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("RegisterProjectionAny typed nil error = %v, want InvalidInput", err)
	}
}

func TestProjectionManager_ReleaseDistinguishesEqualValues(t *testing.T) {
	ctx := context.Background()
	manager, err := NewProjectionManager[int64](nil, &MockEventBus{}, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	owned := comparableProjection{name: "equal"}
	release, err := manager.RegisterProjectionAny(ctx, owned)
	if err != nil || release == nil {
		t.Fatalf("registration = %v, release missing = %v", err, release == nil)
	}
	if err := manager.UnregisterProjection(owned.Name()); err != nil {
		t.Fatal(err)
	}
	currentRelease, err := manager.RegisterProjectionAny(ctx, owned)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ProjectionStatus(owned.Name()); err != nil {
		t.Fatalf("stale release removed equal replacement: %v", err)
	}
	if err := currentRelease(ctx); err != nil {
		t.Fatal(err)
	}
}

type comparableProjection struct {
	name string
}

func (p comparableProjection) Name() string { return p.name }

func (p comparableProjection) Handle(context.Context, eventing.IEvent) error { return nil }

func (p comparableProjection) SupportedEventTypes() []string { return []string{"Event"} }

func (p comparableProjection) Rebuild(context.Context, []eventing.Event[int64]) error { return nil }

func (p comparableProjection) Status() ProjectionStatus { return ProjectionStatus{Name: p.Name()} }

func TestProjectionManager_PartialRegistrationRetainsResourcesForRetry(t *testing.T) {
	rollbackErr := stderrors.New("rollback unsubscribe failed")
	subscribeErr := stderrors.New("second subscription failed")
	eventBus := &failingSubscribeEventBus{
		MockEventBus: &MockEventBus{unsubscribeErr: rollbackErr},
		failType:     "B",
		failErr:      subscribeErr,
	}
	manager, err := NewProjectionManager[int64](store.NewMemoryEventStore[int64](), eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}

	release, err := manager.RegisterProjectionAny(context.Background(), NewMockProjection("partial", []string{"A", "A", "B"}))
	if !stderrors.Is(err, subscribeErr) || !stderrors.Is(err, rollbackErr) {
		t.Fatalf("expected subscription and rollback errors, got %v", err)
	}
	if release == nil {
		t.Fatal("partial registration did not return a live ownership release")
	}
	status, statusErr := manager.ProjectionStatus("partial")
	if statusErr != nil || status.Status != "cleanup_pending" || !strings.Contains(status.LastError, rollbackErr.Error()) {
		t.Fatalf("partial registration status = %+v, error = %v", status, statusErr)
	}
	if err := manager.RegisterProjection(NewMockProjection("partial", []string{"A"})); !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected partial registration name conflict, got %v", err)
	}

	eventBus.unsubscribeErr = nil
	if err := release(context.Background()); err != nil {
		t.Fatalf("cleanup retry failed: %v", err)
	}
	if _, err := manager.ProjectionStatus("partial"); !errors.Is(err, errors.NotFound) {
		t.Fatalf("partial registration survived cleanup: %v", err)
	}
	if got := len(eventBus.handlers["A"]); got != 0 {
		t.Fatalf("subscriptions after rollback retry = %d, want 0", got)
	}
	if err := manager.RegisterProjection(NewMockProjection("partial", []string{"A"})); err != nil {
		t.Fatalf("registration after cleanup failed: %v", err)
	}
}

type registrationRollbackEventBus struct {
	*MockEventBus
	onSubscribed func()
	onRollback   func()
}

func (b *registrationRollbackEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	unsub, err := b.MockEventBus.SubscribeEvent(ctx, eventType, handler)
	if err != nil {
		return nil, err
	}
	if b.onSubscribed != nil {
		fn := b.onSubscribed
		b.onSubscribed = nil
		fn()
	}
	return func(ctx context.Context) error {
		if b.onRollback != nil {
			fn := b.onRollback
			b.onRollback = nil
			fn()
		}
		return unsub(ctx)
	}, nil
}

func TestProjectionManager_FinalValidationRollbackReservesName(t *testing.T) {
	for _, failCleanup := range []bool{false, true} {
		t.Run(map[bool]string{false: "cleanup succeeds", true: "cleanup needs retry"}[failCleanup], func(t *testing.T) {
			ctx := context.Background()
			rollbackErr := stderrors.New("unsubscribe unavailable")
			eventBus := &registrationRollbackEventBus{MockEventBus: &MockEventBus{}}
			if failCleanup {
				eventBus.unsubscribeErr = rollbackErr
			}
			manager, err := NewProjectionManager[int64](store.NewMemoryEventStore[int64](), eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
			if err != nil {
				t.Fatal(err)
			}
			// 在订阅和最终校验之间切换配置，触发注册回滚。
			eventBus.onSubscribed = func() {
				if _, err := manager.WithCheckpointStore(NewMemoryCheckpointStore()); err != nil {
					t.Fatal(err)
				}
			}
			eventBus.onRollback = func() {
				if _, err := manager.WithCheckpointStore(nil); err != nil {
					t.Fatal(err)
				}
				// 退订回调模拟回滚期间发生的同名注册，不依赖调度时序。
				if _, err := manager.RegisterProjectionAny(ctx, comparableProjection{name: "reserved"}); !errors.Is(err, errors.Conflict) {
					t.Errorf("registration during rollback = %v, want Conflict", err)
				}
			}
			release, err := manager.RegisterProjectionAny(ctx, comparableProjection{name: "reserved"})
			if !errors.Is(err, errors.Unsupported) {
				t.Fatalf("registration error = %v, want checkpoint validation failure", err)
			}
			if failCleanup {
				if !stderrors.Is(err, rollbackErr) || release == nil {
					t.Fatalf("failed rollback lost its error or ownership release: %v", err)
				}
				if err := release(ctx); err != nil {
					t.Fatalf("cleanup retry failed: %v", err)
				}
			} else if release != nil {
				t.Fatal("successful rollback retained a release")
			}
			if len(eventBus.handlers["Event"]) != 0 {
				t.Fatal("original subscription leaked")
			}
			if err := manager.RegisterProjection(NewMockProjection("reserved", []string{"Replacement"})); err != nil {
				t.Fatalf("name was not released after cleanup: %v", err)
			}
		})
	}
}

func TestProjectionManager_ReleaseIsIdempotentAndPreservesReplacement(t *testing.T) {
	ctx := context.Background()
	manager, err := NewProjectionManager[int64](nil, &MockEventBus{}, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	release, err := manager.RegisterProjectionAny(ctx, NewMockProjection("shared", []string{"Event"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := release(nil); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("nil context = %v", err)
	}
	if _, err := manager.ProjectionStatus("shared"); err != nil {
		t.Fatal(err)
	}
	if err := release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := release(ctx); err != nil {
		t.Fatalf("repeated release: %v", err)
	}
	if _, err := manager.ProjectionStatus("shared"); !errors.Is(err, errors.NotFound) {
		t.Fatalf("released registration remains: %v", err)
	}
	currentRelease, err := manager.RegisterProjectionAny(ctx, NewMockProjection("shared", []string{"Event"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ProjectionStatus("shared"); err != nil {
		t.Fatalf("stale release removed replacement: %v", err)
	}
	if err := currentRelease(ctx); err != nil {
		t.Fatal(err)
	}
}

type blockingUnsubscribeEventBus struct {
	*MockEventBus
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingUnsubscribeEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	unsub, err := b.MockEventBus.SubscribeEvent(ctx, eventType, handler)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		b.once.Do(func() { close(b.entered) })
		<-b.release
		return unsub(ctx)
	}, nil
}

func TestProjectionManager_CleanupReservesRuntimeAndStopsOldHandler(t *testing.T) {
	eventBus := &blockingUnsubscribeEventBus{
		MockEventBus: &MockEventBus{},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	eventStore := store.NewMemoryEventStore[int64]()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, registry.NewRegistry(), upcast.NewUpgraderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	projection := NewMockProjection("cleanup-gate", []string{"CleanupEvent"})
	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatal(err)
	}
	if err := manager.StartProjection(projection.Name()); err != nil {
		t.Fatal(err)
	}
	rt, ok := manager.runtime(projection.Name())
	if !ok {
		t.Fatal("runtime not found")
	}
	handler := rt.handlers["CleanupEvent"]
	if handler == nil {
		t.Fatal("handler not found")
	}

	unregisterDone := make(chan error, 1)
	go func() { unregisterDone <- manager.UnregisterProjection(projection.Name()) }()
	select {
	case <-eventBus.entered:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not enter")
	}
	if status := manager.ProjectionStatuses()[projection.Name()]; status.Status != "cleanup_pending" {
		t.Errorf("in-progress cleanup status = %+v", status)
	}

	for name, call := range map[string]func() error{
		"start":   func() error { return manager.StartProjection(projection.Name()) },
		"resume":  func() error { return manager.ResumeFromCheckpoint(context.Background(), projection.Name()) },
		"rebuild": func() error { return manager.RebuildProjection(context.Background(), projection.Name(), nil) },
	} {
		if err := call(); !errors.Is(err, errors.Conflict) {
			t.Errorf("%s during cleanup error = %v, want Conflict", name, err)
		}
	}

	if err := handler.HandleEvent(context.Background(), testutil.NewEvent[int64](1, "Agg", "CleanupEvent", 1, nil)); err != nil {
		t.Fatalf("old handler returned error after deactivation: %v", err)
	}
	if projection.processedEvents != 0 {
		t.Fatalf("old handler consumed event after deactivation: %d", projection.processedEvents)
	}

	close(eventBus.release)
	select {
	case err := <-unregisterDone:
		if err != nil {
			t.Fatalf("unregister failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unregister did not finish")
	}
	if err := manager.RegisterProjection(NewMockProjection(projection.Name(), []string{"CleanupEvent"})); err != nil {
		t.Fatalf("name was not reusable after cleanup: %v", err)
	}
}

type failingSubscribeEventBus struct {
	*MockEventBus
	failType string
	failErr  error
}

func (b *failingSubscribeEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	if eventType == b.failType {
		return nil, b.failErr
	}
	return b.MockEventBus.SubscribeEvent(ctx, eventType, handler)
}

// TestProjectionManager_RegisterProjection 验证 ProjectionManager RegisterProjection。
func TestProjectionManager_RegisterProjection(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	projection := NewMockProjection("test-projection", []string{"TestEvent"})

	err = manager.RegisterProjection(projection)

	assert.NoError(t, err)
	_, err = manager.ProjectionStatus("test-projection")
	assert.NoError(t, err)
}

// TestProjectionManager_RegisterNilProjection 验证 ProjectionManager RegisterNilProjection。
func TestProjectionManager_RegisterNilProjection(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	err = manager.RegisterProjection(nil)
	assert.Error(t, err)
}

func TestProjectionManager_RegisterProjectionRejectsNilEventBus(t *testing.T) {
	manager := &ProjectionManager[int64]{
		runtimes: make(map[string]*projectionRuntime[int64]),
		eventBus: nil,
		config:   DefaultProjectionConfig(),
	}

	err := manager.RegisterProjection(NewMockProjection("test-projection", []string{"TestEvent"}))

	assert.Error(t, err)
	assert.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got: %v", err)
	assert.Contains(t, err.Error(), "event bus")
}

// TestProjectionManager_GetProjectionStatus 验证 ProjectionManager ProjectionStatus。
func TestProjectionManager_GetProjectionStatus(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	manager.RegisterProjection(projection)

	status, err := manager.ProjectionStatus("test-projection")

	assert.NoError(t, err)
	assert.Equal(t, "test-projection", status.Name)
	// Status is "stopped" until StartProjection is called
	assert.Contains(t, []string{"running", "stopped"}, status.Status)
}

// TestProjectionManager_GetProjectionStatus_NotFound 验证 ProjectionManager ProjectionStatus NotFound。
func TestProjectionManager_GetProjectionStatus_NotFound(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	_, err = manager.ProjectionStatus("non-existent")

	assert.Error(t, err)
}

// TestProjectionManager_MultipleProjections 验证 ProjectionManager MultipleProjections。
func TestProjectionManager_MultipleProjections(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	// Register multiple projections
	proj1 := NewMockProjection("projection-1", []string{"Event1"})
	proj2 := NewMockProjection("projection-2", []string{"Event2"})

	err1 := manager.RegisterProjection(proj1)
	err2 := manager.RegisterProjection(proj2)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, manager.ProjectionStatuses(), 2)
}

// TestProjectionManager_MultipleProjectionsSameEventType 验证 ProjectionManager MultipleProjectionsSameEventType。
func TestProjectionManager_MultipleProjectionsSameEventType(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	// Both projections handle same event type
	proj1 := NewMockProjection("projection-1", []string{"SharedEvent"})
	proj2 := NewMockProjection("projection-2", []string{"SharedEvent"})

	err = manager.RegisterProjection(proj1)
	assert.NoError(t, err)

	err = manager.RegisterProjection(proj2)
	assert.NoError(t, err)

	// Both should be registered
	assert.Len(t, manager.ProjectionStatuses(), 2)
}

// TestProjectionManager_ProjectionMultipleEventTypes 验证 ProjectionManager ProjectionMultipleEventTypes。
func TestProjectionManager_ProjectionMultipleEventTypes(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	projection := NewMockProjection("multi-type-projection", []string{"Event1", "Event2", "Event3"})

	err = manager.RegisterProjection(projection)

	assert.NoError(t, err)
	assert.Len(t, projection.SupportedEventTypes(), 3)
}

// BenchmarkProjectionManager_RegisterProjection 用于评估 ProjectionManager RegisterProjection 的性能。
func BenchmarkProjectionManager_RegisterProjection(b *testing.B) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	if err != nil {
		b.Fatalf("NewProjectionManager failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		projection := NewMockProjection("bench-projection", []string{"BenchEvent"})
		manager.RegisterProjection(projection)
	}
}
