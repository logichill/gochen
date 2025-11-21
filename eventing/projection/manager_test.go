package projection

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/messaging"
)

// MockProjection 测试用投影
type MockProjection struct {
	name            string
	supportedTypes  []string
	handleFunc      func(ctx context.Context, event eventing.IEvent) error
	rebuildFunc     func(ctx context.Context, events []eventing.Event) error
	processedEvents int
	failedEvents    int
	status          string
	mu              sync.Mutex
}

func NewMockProjection(name string, types []string) *MockProjection {
	return &MockProjection{
		name:           name,
		supportedTypes: types,
		status:         "running",
	}
}

func (p *MockProjection) GetName() string {
	return p.name
}

func (p *MockProjection) Handle(ctx context.Context, event eventing.IEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.handleFunc != nil {
		err := p.handleFunc(ctx, event)
		if err != nil {
			p.failedEvents++
			return err
		}
	}
	p.processedEvents++
	return nil
}

func (p *MockProjection) GetSupportedEventTypes() []string {
	return p.supportedTypes
}

func (p *MockProjection) Rebuild(ctx context.Context, events []eventing.Event) error {
	if p.rebuildFunc != nil {
		return p.rebuildFunc(ctx, events)
	}
	return nil
}

func (p *MockProjection) GetStatus() ProjectionStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	return ProjectionStatus{
		Name:            p.name,
		ProcessedEvents: int64(p.processedEvents),
		FailedEvents:    int64(p.failedEvents),
		Status:          p.status,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// Test 1: Create projection manager
func TestNewProjectionManager(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	// EventBus requires transport, use simple mock for now
	eventBus := &MockEventBus{}

	manager := NewProjectionManager(eventStore, eventBus)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.projections)
	assert.NotNil(t, manager.config)
}

// MockEventBus for testing
type MockEventBus struct {
	publishedEvents []eventing.IEvent
	handlers        map[string][]bus.IEventHandler
	mu              sync.Mutex
}

func (m *MockEventBus) PublishEvent(ctx context.Context, event eventing.IEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishedEvents == nil {
		m.publishedEvents = []eventing.IEvent{}
	}
	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}

func (m *MockEventBus) PublishEvents(ctx context.Context, events []eventing.IEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishedEvents == nil {
		m.publishedEvents = []eventing.IEvent{}
	}

	// Simply append the events (they are already interfaces)
	m.publishedEvents = append(m.publishedEvents, events...)
	return nil
}

func (m *MockEventBus) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	// Convert messages to events and publish
	for _, msg := range messages {
		if evt, ok := msg.(eventing.IEvent); ok {
			m.PublishEvent(ctx, evt)
		}
	}
	return nil
}

func (m *MockEventBus) SubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.handlers == nil {
		m.handlers = make(map[string][]bus.IEventHandler)
	}
	m.handlers[eventType] = append(m.handlers[eventType], handler)
	return nil
}

func (m *MockEventBus) UnsubscribeEvent(ctx context.Context, eventType string, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) SubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) UnsubscribeHandler(ctx context.Context, handler bus.IEventHandler) error {
	return nil
}

func (m *MockEventBus) Publish(ctx context.Context, message messaging.IMessage) error {
	return nil
}

func (m *MockEventBus) Subscribe(ctx context.Context, topic string, handler messaging.IMessageHandler) error {
	return nil
}

func (m *MockEventBus) Unsubscribe(ctx context.Context, topic string, handler messaging.IMessageHandler) error {
	return nil
}

func (m *MockEventBus) Use(middleware messaging.IMiddleware) {
	// No-op for mock
}

// toStorableEvents 将事件切片转换为存储接口切片（测试辅助）
func toStorableEvents(events []eventing.Event) []eventing.IStorableEvent {
	storable := make([]eventing.IStorableEvent, len(events))
	for i := range events {
		storable[i] = &events[i]
	}
	return storable
}

// Test 2: Register projection
func TestProjectionManager_RegisterProjection(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	projection := NewMockProjection("test-projection", []string{"TestEvent"})

	err := manager.RegisterProjection(projection)

	assert.NoError(t, err)
	assert.Contains(t, manager.projections, "test-projection")
}

// Test 3: Register nil projection should return error
func TestProjectionManager_RegisterNilProjection(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	err := manager.RegisterProjection(nil)
	assert.Error(t, err)
}

// Test 4: Get projection status - success
func TestProjectionManager_GetProjectionStatus(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	manager.RegisterProjection(projection)

	status, err := manager.GetProjectionStatus("test-projection")

	assert.NoError(t, err)
	assert.Equal(t, "test-projection", status.Name)
	// Status is "stopped" until StartProjection is called
	assert.Contains(t, []string{"running", "stopped"}, status.Status)
}

// Test 5: Get projection status - not found
func TestProjectionManager_GetProjectionStatus_NotFound(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	_, err := manager.GetProjectionStatus("non-existent")

	assert.Error(t, err)
}

// Test 6: Multiple projections registered
func TestProjectionManager_MultipleProjections(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	// Register multiple projections
	proj1 := NewMockProjection("projection-1", []string{"Event1"})
	proj2 := NewMockProjection("projection-2", []string{"Event2"})

	err1 := manager.RegisterProjection(proj1)
	err2 := manager.RegisterProjection(proj2)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Len(t, manager.projections, 2)
}

// Test 7: Custom configuration
func TestProjectionManager_CustomConfig(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	config := &ProjectionConfig{
		MaxRetries:   5,
		RetryBackoff: 2 * time.Second,
	}

	manager := NewProjectionManagerWithConfig(eventStore, eventBus, config)

	assert.NotNil(t, manager)
	assert.Equal(t, 5, manager.config.MaxRetries)
	assert.Equal(t, 2*time.Second, manager.config.RetryBackoff)
}

// Test 8: Default configuration
func TestDefaultProjectionConfig(t *testing.T) {
	config := DefaultProjectionConfig()

	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.RetryBackoff)
	assert.NotNil(t, config.DeadLetterFunc)
}

// Test 9: Multiple projections with same event type
func TestProjectionManager_MultipleProjectionsSameEventType(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	// Both projections handle same event type
	proj1 := NewMockProjection("projection-1", []string{"SharedEvent"})
	proj2 := NewMockProjection("projection-2", []string{"SharedEvent"})

	err := manager.RegisterProjection(proj1)
	assert.NoError(t, err)

	err = manager.RegisterProjection(proj2)
	assert.NoError(t, err)

	// Both should be registered
	assert.Len(t, manager.projections, 2)
}

// Test 10: Projection with multiple event types
func TestProjectionManager_ProjectionMultipleEventTypes(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	projection := NewMockProjection("multi-type-projection", []string{"Event1", "Event2", "Event3"})

	err := manager.RegisterProjection(projection)

	assert.NoError(t, err)
	assert.Len(t, projection.GetSupportedEventTypes(), 3)
}

// Benchmark projection manager
func BenchmarkProjectionManager_RegisterProjection(b *testing.B) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		projection := NewMockProjection("bench-projection", []string{"BenchEvent"})
		manager.RegisterProjection(projection)
	}
}

// Test 11: projectionEventHandler respects running status
func TestProjectionEventHandler_ShouldProcessOnlyWhenRunning(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	manager := NewProjectionManager(eventStore, eventBus)

	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	err := manager.RegisterProjection(projection)
	assert.NoError(t, err)

	handler := &projectionEventHandler{
		projection: projection,
		manager:    manager,
	}

	evt := &eventing.Event{
		Message: messaging.Message{
			ID:        "event-1",
			Type:      "TestEvent",
			Timestamp: time.Now(),
			Metadata:  make(map[string]any),
		},
	}

	// stopped 状态下不应处理事件
	err = handler.HandleEvent(context.Background(), evt)
	assert.NoError(t, err)
	assert.Equal(t, 0, projection.processedEvents)

	// 启动后应处理事件
	err = manager.StartProjection("test-projection")
	assert.NoError(t, err)

	err = handler.HandleEvent(context.Background(), evt)
	assert.NoError(t, err)
	assert.Equal(t, 1, projection.processedEvents)
}

// Test 12: ResumeFromCheckpoint should replay events from EventStore after checkpoint
func TestProjectionManager_ResumeFromCheckpoint_ReplaysFromStore(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()

	manager := NewProjectionManager(eventStore, eventBus).WithCheckpointStore(checkpointStore)
	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	require.NoError(t, manager.RegisterProjection(projection))

	events := []eventing.Event{
		*eventing.NewEvent(1, "Agg", "TestEvent", 1, map[string]any{"i": 1}),
		*eventing.NewEvent(1, "Agg", "TestEvent", 2, map[string]any{"i": 2}),
		*eventing.NewEvent(1, "Agg", "TestEvent", 3, map[string]any{"i": 3}),
	}
	// 统一时间戳，验证同时间戳下不会重复重放 checkpoint 前的事件
	now := time.Now()
	for i := range events {
		events[i].Timestamp = now
	}
	require.NoError(t, eventStore.AppendEvents(ctx, 1, toStorableEvents(events), 0))

	// checkpoint 在第二个事件
	checkpoint := NewCheckpoint("test-projection", 2, events[1].ID, events[1].Timestamp)
	require.NoError(t, checkpointStore.Save(ctx, checkpoint))

	require.NoError(t, manager.ResumeFromCheckpoint(ctx, "test-projection"))

	// 仅应重放 checkpoint 之后的事件
	assert.Equal(t, 1, projection.processedEvents)

	status, err := manager.GetProjectionStatus("test-projection")
	require.NoError(t, err)
	assert.Equal(t, int64(3), status.ProcessedEvents)
	assert.Equal(t, events[2].ID, status.LastEventID)
}

// Test 13: Replay failure should mark status error and stop processing
func TestProjectionManager_ResumeFromCheckpoint_ReplayFailureStops(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()

	manager := NewProjectionManager(eventStore, eventBus).WithCheckpointStore(checkpointStore)
	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	// 处理时故意失败
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		return fmt.Errorf("fail:%s", event.GetID())
	}
	require.NoError(t, manager.RegisterProjection(projection))

	e1 := eventing.NewEvent(1, "Agg", "TestEvent", 1, nil)
	e2 := eventing.NewEvent(1, "Agg", "TestEvent", 2, nil)
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now
	require.NoError(t, eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent{e1, e2}, 0))

	// checkpoint 在第一个事件
	checkpoint := NewCheckpoint("test-projection", 1, e1.ID, e1.Timestamp)
	require.NoError(t, checkpointStore.Save(ctx, checkpoint))

	err := manager.ResumeFromCheckpoint(ctx, "test-projection")
	require.Error(t, err)

	status, sErr := manager.GetProjectionStatus("test-projection")
	require.NoError(t, sErr)
	assert.Equal(t, "error", status.Status)
	assert.Equal(t, int64(1), status.ProcessedEvents) // 未推进
	assert.Contains(t, status.LastError, "fail")
}

// Test 14: 重放阶段重试成功（首次失败，后续重试成功）
func TestProjectionManager_ReplayRetry_Success(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	cfg := &ProjectionConfig{
		MaxRetries:   2,              // 允许最多重试 2 次（总尝试 <=3）
		RetryBackoff: 0 * time.Millisecond,
	}
	manager := NewProjectionManagerWithConfig(eventStore, eventBus, cfg)

	var attempts int
	projection := NewMockProjection("retry-projection", []string{"TestEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		attempts++
		if attempts == 1 {
			return fmt.Errorf("fail-once:%s", event.GetID())
		}
		return nil
	}
	require.NoError(t, manager.RegisterProjection(projection))
	manager.checkpointStore = NewMemoryCheckpointStore()

	evt := eventing.NewEvent(1, "Agg", "TestEvent", 1, nil)
	err := manager.applyReplayEvent(ctx, "retry-projection", projection, evt)
	require.NoError(t, err)

	// 第一次失败 + 第二次成功
	assert.Equal(t, 2, attempts)

	status, sErr := manager.GetProjectionStatus("retry-projection")
	require.NoError(t, sErr)
	assert.Equal(t, int64(1), status.ProcessedEvents)
	assert.Equal(t, int64(0), status.FailedEvents)
	assert.Equal(t, evt.ID, status.LastEventID)
}

// Test 15: 重放阶段重试超过上限仍失败
func TestProjectionManager_ReplayRetry_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	cfg := &ProjectionConfig{
		MaxRetries:   2,              // 最多重试 2 次（总尝试 3 次）
		RetryBackoff: 0 * time.Millisecond,
	}
	manager := NewProjectionManagerWithConfig(eventStore, eventBus, cfg)

	var attempts int
	projection := NewMockProjection("retry-projection", []string{"TestEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		attempts++
		return fmt.Errorf("always-fail:%s", event.GetID())
	}
	require.NoError(t, manager.RegisterProjection(projection))

	evt := eventing.NewEvent(1, "Agg", "TestEvent", 1, nil)
	err := manager.applyReplayEvent(ctx, "retry-projection", projection, evt)
	require.Error(t, err)

	// 初始 + 2 次重试，共 3 次
	assert.Equal(t, 3, attempts)

	status, sErr := manager.GetProjectionStatus("retry-projection")
	require.NoError(t, sErr)
	// 重放失败时不会推进 ProcessedEvents，只统计 FailedEvents
	assert.Equal(t, int64(0), status.ProcessedEvents)
	assert.Equal(t, int64(1), status.FailedEvents)
	assert.Contains(t, status.LastError, "always-fail")
}

// Test 16: 重放阶段在重试 backoff 中被 context 取消
func TestProjectionManager_ReplayRetry_ContextCanceled(t *testing.T) {
	// 使用带取消的上下文，并在调用前即取消，确保在 backoff 阶段通过 ctx.Done 中断
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	cfg := &ProjectionConfig{
		MaxRetries:   2,
		RetryBackoff: 10 * time.Millisecond,
	}
	manager := NewProjectionManagerWithConfig(eventStore, eventBus, cfg)

	var attempts int
	projection := NewMockProjection("retry-projection", []string{"TestEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		attempts++
		return fmt.Errorf("cancelled:%s", event.GetID())
	}
	require.NoError(t, manager.RegisterProjection(projection))

	evt := eventing.NewEvent(1, "Agg", "TestEvent", 1, nil)
	err := manager.applyReplayEvent(ctx, "retry-projection", projection, evt)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// 上下文在首次失败后即取消，不会进入下一轮重试
	assert.Equal(t, 1, attempts)

	status, sErr := manager.GetProjectionStatus("retry-projection")
	require.NoError(t, sErr)
	// 因为在状态更新前即返回，Processed/Failed 都不应被修改
	assert.Equal(t, int64(0), status.ProcessedEvents)
	assert.Equal(t, int64(0), status.FailedEvents)
}
