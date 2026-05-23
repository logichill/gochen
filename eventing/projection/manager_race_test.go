package projection

import (
	"context"
	"sync"
	"testing"
	"time"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

// TestProjectionEventHandler_ConcurrentHandleEvent 验证 ProjectionEventHandler ConcurrentHandleEvent。
func TestProjectionEventHandler_ConcurrentHandleEvent(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	if err := reg.Register("ConcurrentEvent", func() any { return &struct{}{} }); err != nil {
		t.Fatalf("register event type failed: %v", err)
	}
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}

	projection := NewMockProjection("concurrent-projection", []string{"ConcurrentEvent"})

	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}

	if err := manager.StartProjection("concurrent-projection"); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}

	// 构造事件
	evt := &eventing.Event[int64]{
		Message: messaging.Message{
			ID:        "evt-1",
			Type:      "ConcurrentEvent",
			Timestamp: time.Now(),
			Metadata:  messaging.NewMetadata(),
		},
	}

	// 从 manager 中取出对应的 handler
	rt, ok := manager.runtime("concurrent-projection")
	if !ok {
		t.Fatalf("expected runtime for projection")
	}
	handler := rt.handlers["ConcurrentEvent"]

	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	const (
		goroutines = 16
		perGor     = 50
	)

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGor; i++ {
				_ = handler.HandleEvent(ctx, evt)
			}
		}()
	}

	wg.Wait()

	status, err := manager.ProjectionStatus("concurrent-projection")
	if err != nil {
		t.Fatalf("ProjectionStatus failed: %v", err)
	}

	if status.ProcessedEvents <= 0 {
		t.Fatalf("expected some processed events, got %d", status.ProcessedEvents)
	}
}

func TestProjectionEventHandler_CheckpointMode_CatchesUpFromStoreOrder(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	if err := reg.Register("ConcurrentEvent", func() any { return &struct{}{} }); err != nil {
		t.Fatalf("register event type failed: %v", err)
	}
	upgraders := upcast.NewUpgraderRegistry()
	cfg := &ProjectionConfig{
		MaxRetries:             0,
		RetryBackoff:           0,
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    1,
	}
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}
	checkpointStore := NewMemoryCheckpointStore()
	manager, err = manager.WithCheckpointStore(checkpointStore)
	if err != nil {
		t.Fatalf("WithCheckpointStore failed: %v", err)
	}

	var mu sync.Mutex
	handled := make([]string, 0, 2)
	projection := NewMockProjection("ordered-checkpoint-projection", []string{"ConcurrentEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		mu.Lock()
		handled = append(handled, event.GetID())
		mu.Unlock()
		return nil
	}

	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}
	if err := manager.StartProjection(projection.Name()); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}

	evt1 := eventing.NewEvent[int64](1, "Agg", "ConcurrentEvent", 1, nil)
	evt2 := eventing.NewEvent[int64](1, "Agg", "ConcurrentEvent", 2, nil)
	if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt1}, 0); err != nil {
		t.Fatalf("append first event failed: %v", err)
	}
	if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt2}, 1); err != nil {
		t.Fatalf("append second event failed: %v", err)
	}

	rt, ok := manager.runtime(projection.Name())
	if !ok {
		t.Fatalf("expected runtime for projection")
	}
	handler := rt.handlers["ConcurrentEvent"]
	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	if err := handler.HandleEvent(ctx, evt2); err != nil {
		t.Fatalf("out-of-order handle failed: %v", err)
	}

	mu.Lock()
	gotHandled := append([]string(nil), handled...)
	mu.Unlock()
	if len(gotHandled) != 2 || gotHandled[0] != evt1.ID || gotHandled[1] != evt2.ID {
		t.Fatalf("expected store-order catch-up [%s %s], got %#v", evt1.ID, evt2.ID, gotHandled)
	}

	checkpoint, err := checkpointStore.Load(ctx, projection.Name())
	if err != nil {
		t.Fatalf("load checkpoint failed: %v", err)
	}
	if checkpoint.Position != 2 || checkpoint.LastEventID != evt2.ID {
		t.Fatalf("expected checkpoint at second event, got position=%d id=%s", checkpoint.Position, checkpoint.LastEventID)
	}
}

func TestProjectionEventHandler_CheckpointMode_UsesRuntimeCursorBetweenDurableSaves(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	if err := reg.Register("RuntimeCursorEvent", func() any { return &struct{}{} }); err != nil {
		t.Fatalf("register event type failed: %v", err)
	}
	upgraders := upcast.NewUpgraderRegistry()
	cfg := &ProjectionConfig{
		MaxRetries:             0,
		RetryBackoff:           0,
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    3,
	}
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}
	checkpointStore := NewMemoryCheckpointStore()
	manager, err = manager.WithCheckpointStore(checkpointStore)
	if err != nil {
		t.Fatalf("WithCheckpointStore failed: %v", err)
	}

	var mu sync.Mutex
	handled := make([]string, 0, 3)
	projection := NewMockProjection("runtime-cursor-projection", []string{"RuntimeCursorEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		mu.Lock()
		handled = append(handled, event.GetID())
		mu.Unlock()
		return nil
	}

	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}
	if err := manager.StartProjection(projection.Name()); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}
	rt, ok := manager.runtime(projection.Name())
	if !ok {
		t.Fatalf("expected runtime for projection")
	}
	handler := rt.handlers["RuntimeCursorEvent"]
	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	events := make([]*eventing.Event[int64], 0, 3)
	for i := 1; i <= 3; i++ {
		evt := eventing.NewEvent[int64](1, "Agg", "RuntimeCursorEvent", uint64(i), nil)
		events = append(events, evt)
		if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)); err != nil {
			t.Fatalf("append event %d failed: %v", i, err)
		}
		if err := handler.HandleEvent(ctx, evt); err != nil {
			t.Fatalf("handle event %d failed: %v", i, err)
		}
	}

	mu.Lock()
	gotHandled := append([]string(nil), handled...)
	mu.Unlock()
	want := []string{events[0].ID, events[1].ID, events[2].ID}
	if len(gotHandled) != len(want) {
		t.Fatalf("expected %d handled events, got %#v", len(want), gotHandled)
	}
	for i := range want {
		if gotHandled[i] != want[i] {
			t.Fatalf("expected handled order %#v, got %#v", want, gotHandled)
		}
	}

	checkpoint, err := checkpointStore.Load(ctx, projection.Name())
	if err != nil {
		t.Fatalf("load checkpoint failed: %v", err)
	}
	if checkpoint.Position != 3 || checkpoint.LastEventID != events[2].ID {
		t.Fatalf("expected checkpoint at third event, got position=%d id=%s", checkpoint.Position, checkpoint.LastEventID)
	}
}

func TestProjectionManager_ResumeFromCheckpoint_UsesRuntimeCursorWhenDurableBehind(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	if err := reg.Register("RuntimeResumeCursorEvent", func() any { return &struct{}{} }); err != nil {
		t.Fatalf("register event type failed: %v", err)
	}
	upgraders := upcast.NewUpgraderRegistry()
	cfg := &ProjectionConfig{
		MaxRetries:             0,
		RetryBackoff:           0,
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    3,
	}
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}
	checkpointStore := NewMemoryCheckpointStore()
	manager, err = manager.WithCheckpointStore(checkpointStore)
	if err != nil {
		t.Fatalf("WithCheckpointStore failed: %v", err)
	}

	var mu sync.Mutex
	handled := make([]string, 0, 3)
	projection := NewMockProjection("runtime-resume-cursor-projection", []string{"RuntimeResumeCursorEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		mu.Lock()
		handled = append(handled, event.GetID())
		mu.Unlock()
		return nil
	}

	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}
	if err := manager.StartProjection(projection.Name()); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}
	rt, ok := manager.runtime(projection.Name())
	if !ok {
		t.Fatalf("expected runtime for projection")
	}
	handler := rt.handlers["RuntimeResumeCursorEvent"]
	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	events := make([]*eventing.Event[int64], 0, 3)
	for i := 1; i <= 2; i++ {
		evt := eventing.NewEvent[int64](1, "Agg", "RuntimeResumeCursorEvent", uint64(i), nil)
		events = append(events, evt)
		if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)); err != nil {
			t.Fatalf("append event %d failed: %v", i, err)
		}
		if err := handler.HandleEvent(ctx, evt); err != nil {
			t.Fatalf("handle event %d failed: %v", i, err)
		}
	}
	if _, err := checkpointStore.Load(ctx, projection.Name()); err == nil {
		t.Fatalf("expected durable checkpoint to be absent before save threshold")
	}

	if err := manager.ResumeFromCheckpoint(ctx, projection.Name()); err != nil {
		t.Fatalf("resume from checkpoint failed: %v", err)
	}

	evt3 := eventing.NewEvent[int64](1, "Agg", "RuntimeResumeCursorEvent", 3, nil)
	events = append(events, evt3)
	if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt3}, 2); err != nil {
		t.Fatalf("append third event failed: %v", err)
	}
	if err := handler.HandleEvent(ctx, evt3); err != nil {
		t.Fatalf("handle third event failed: %v", err)
	}

	mu.Lock()
	gotHandled := append([]string(nil), handled...)
	mu.Unlock()
	want := []string{events[0].ID, events[1].ID, events[2].ID}
	if len(gotHandled) != len(want) {
		t.Fatalf("expected %d handled events, got %#v", len(want), gotHandled)
	}
	for i := range want {
		if gotHandled[i] != want[i] {
			t.Fatalf("expected handled order %#v, got %#v", want, gotHandled)
		}
	}
}

func TestProjectionEventHandler_CheckpointMode_RequiresRunning(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	if err := reg.Register("StoppedCheckpointEvent", func() any { return &struct{}{} }); err != nil {
		t.Fatalf("register event type failed: %v", err)
	}
	upgraders := upcast.NewUpgraderRegistry()
	cfg := &ProjectionConfig{
		MaxRetries:             0,
		RetryBackoff:           0,
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    1,
	}
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}
	checkpointStore := NewMemoryCheckpointStore()
	manager, err = manager.WithCheckpointStore(checkpointStore)
	if err != nil {
		t.Fatalf("WithCheckpointStore failed: %v", err)
	}

	projection := NewMockProjection("stopped-checkpoint-projection", []string{"StoppedCheckpointEvent"})
	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}
	rt, ok := manager.runtime(projection.Name())
	if !ok {
		t.Fatalf("expected runtime for projection")
	}
	handler := rt.handlers["StoppedCheckpointEvent"]
	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	evt1 := eventing.NewEvent[int64](1, "Agg", "StoppedCheckpointEvent", 1, nil)
	if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt1}, 0); err != nil {
		t.Fatalf("append first event failed: %v", err)
	}
	if err := handler.HandleEvent(ctx, evt1); err != nil {
		t.Fatalf("handle stopped event failed: %v", err)
	}
	if projection.processedEvents != 0 {
		t.Fatalf("expected stopped projection to skip event before start, got %d", projection.processedEvents)
	}

	if err := manager.StartProjection(projection.Name()); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}
	if err := handler.HandleEvent(ctx, evt1); err != nil {
		t.Fatalf("handle running event failed: %v", err)
	}
	if projection.processedEvents != 1 {
		t.Fatalf("expected running projection to process first event once, got %d", projection.processedEvents)
	}
	if err := manager.StopProjection(projection.Name()); err != nil {
		t.Fatalf("stop projection failed: %v", err)
	}

	evt2 := eventing.NewEvent[int64](1, "Agg", "StoppedCheckpointEvent", 2, nil)
	if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt2}, 1); err != nil {
		t.Fatalf("append second event failed: %v", err)
	}
	if err := handler.HandleEvent(ctx, evt2); err != nil {
		t.Fatalf("handle stopped event after stop failed: %v", err)
	}
	if projection.processedEvents != 1 {
		t.Fatalf("expected stopped projection after stop to skip event, got %d", projection.processedEvents)
	}
}

func TestProjectionRuntime_ConcurrentStatusDuringCatchUp_NoRace(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	if err := reg.Register("StatusRaceEvent", func() any { return &struct{}{} }); err != nil {
		t.Fatalf("register event type failed: %v", err)
	}
	upgraders := upcast.NewUpgraderRegistry()
	cfg := &ProjectionConfig{
		MaxRetries:             0,
		RetryBackoff:           0,
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    1,
	}
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}
	manager, err = manager.WithCheckpointStore(NewMemoryCheckpointStore())
	if err != nil {
		t.Fatalf("WithCheckpointStore failed: %v", err)
	}

	projection := NewMockProjection("status-race-projection", []string{"StatusRaceEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		time.Sleep(time.Millisecond)
		return nil
	}
	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}
	if err := manager.StartProjection(projection.Name()); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}
	rt, ok := manager.runtime(projection.Name())
	if !ok {
		t.Fatalf("expected runtime for projection")
	}
	handler := rt.handlers["StatusRaceEvent"]
	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	var first *eventing.Event[int64]
	for i := 1; i <= 8; i++ {
		evt := eventing.NewEvent[int64](1, "Agg", "StatusRaceEvent", uint64(i), nil)
		if first == nil {
			first = evt
		}
		if err := eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{evt}, uint64(i-1)); err != nil {
			t.Fatalf("append event %d failed: %v", i, err)
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = handler.HandleEvent(ctx, first)
	}()

	for {
		select {
		case <-done:
			return
		default:
			if _, err := manager.ProjectionStatus(projection.Name()); err != nil {
				t.Fatalf("ProjectionStatus failed: %v", err)
			}
		}
	}
}

type callbackEventBus struct {
	*MockEventBus
	onSubscribe   func()
	onUnsubscribe func()
}

func (b *callbackEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) (messaging.UnsubscribeFunc, error) {
	if b.onSubscribe != nil {
		b.onSubscribe()
	}
	unsub, err := b.MockEventBus.SubscribeEvent(ctx, eventType, handler)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		if b.onUnsubscribe != nil {
			b.onUnsubscribe()
		}
		return unsub(ctx)
	}, nil
}

func TestProjectionManager_RegisterProjection_SubscribeCallbackDoesNotHoldManagerLock(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &callbackEventBus{MockEventBus: &MockEventBus{}}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}
	eventBus.onSubscribe = func() {
		_ = manager.ProjectionStatuses()
	}

	done := make(chan error, 1)
	go func() {
		done <- manager.RegisterProjection(NewMockProjection("callback-register-projection", []string{"CallbackEvent"}))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register projection failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("register projection blocked while subscribe callback queried manager")
	}
}

func TestProjectionManager_UnregisterProjection_UnsubscribeCallbackDoesNotHoldManagerLock(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &callbackEventBus{MockEventBus: &MockEventBus{}}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}
	if err := manager.RegisterProjection(NewMockProjection("callback-unregister-projection", []string{"CallbackEvent"})); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}
	eventBus.onUnsubscribe = func() {
		_ = manager.ProjectionStatuses()
	}

	done := make(chan error, 1)
	go func() {
		done <- manager.UnregisterProjection("callback-unregister-projection")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unregister projection failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("unregister projection blocked while unsubscribe callback queried manager")
	}
}

func TestProjectionManager_UnregisterProjection_WaitsForInFlightHandler(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	if err := reg.Register("UnregisterWaitEvent", func() any { return &struct{}{} }); err != nil {
		t.Fatalf("register event type failed: %v", err)
	}
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	if err != nil {
		t.Fatalf("NewProjectionManager failed: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	projection := NewMockProjection("unregister-wait-projection", []string{"UnregisterWaitEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		close(entered)
		<-release
		return nil
	}
	if err := manager.RegisterProjection(projection); err != nil {
		t.Fatalf("register projection failed: %v", err)
	}
	if err := manager.StartProjection(projection.Name()); err != nil {
		t.Fatalf("start projection failed: %v", err)
	}
	rt, ok := manager.runtime(projection.Name())
	if !ok {
		t.Fatalf("expected runtime for projection")
	}
	handler := rt.handlers["UnregisterWaitEvent"]
	if handler == nil {
		t.Fatalf("expected handler for projection/event type not found")
	}

	handleDone := make(chan error, 1)
	go func() {
		handleDone <- handler.HandleEvent(ctx, eventing.NewEvent[int64](1, "Agg", "UnregisterWaitEvent", 1, nil))
	}()
	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("handler did not start")
	}

	unregisterDone := make(chan error, 1)
	go func() {
		unregisterDone <- manager.UnregisterProjection(projection.Name())
	}()
	select {
	case err := <-unregisterDone:
		t.Fatalf("unregister returned before in-flight handler completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-handleDone:
		if err != nil {
			t.Fatalf("handler failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("handler did not finish")
	}
	select {
	case err := <-unregisterDone:
		if err != nil {
			t.Fatalf("unregister projection failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("unregister did not finish after handler completed")
	}
}
