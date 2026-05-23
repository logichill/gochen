package projection

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/contextx"
	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

type replayTestPayload struct{}

type checkpointingTxKey struct{}

type checkpointingTxRunner struct {
	calls int
}

func (r *checkpointingTxRunner) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	r.calls++
	return fn(context.WithValue(ctx, checkpointingTxKey{}, true))
}

type nonCheckpointProjection struct {
	name string
}

func (p *nonCheckpointProjection) Name() string { return p.name }
func (p *nonCheckpointProjection) Handle(context.Context, eventing.IEvent) error {
	return nil
}
func (p *nonCheckpointProjection) SupportedEventTypes() []string {
	return []string{"TestEvent"}
}
func (p *nonCheckpointProjection) Rebuild(context.Context, []eventing.Event[int64]) error {
	return nil
}
func (p *nonCheckpointProjection) Status() ProjectionStatus {
	return ProjectionStatus{Name: p.name, Status: "stopped"}
}

type rebuildMissingCheckpointProjection struct {
	inner *MockProjection
}

func (p *rebuildMissingCheckpointProjection) Name() string { return p.inner.Name() }

func (p *rebuildMissingCheckpointProjection) Handle(ctx context.Context, event eventing.IEvent) error {
	return p.inner.Handle(ctx, event)
}

func (p *rebuildMissingCheckpointProjection) HandleWithCheckpoint(ctx context.Context, event eventing.IEvent, store ICheckpointStore, checkpoint *Checkpoint) error {
	return p.inner.HandleWithCheckpoint(ctx, event, store, checkpoint)
}

func (p *rebuildMissingCheckpointProjection) SupportedEventTypes() []string {
	return p.inner.SupportedEventTypes()
}

func (p *rebuildMissingCheckpointProjection) Rebuild(ctx context.Context, events []eventing.Event[int64]) error {
	return p.inner.Rebuild(ctx, events)
}

func (p *rebuildMissingCheckpointProjection) Status() ProjectionStatus {
	return p.inner.Status()
}

func newTestRegistry(tb testing.TB) *registry.Registry {
	tb.Helper()
	reg := registry.NewRegistry()
	require.NoError(tb, reg.Register("TestEvent", func() any { return &replayTestPayload{} }))
	return reg
}

// TestProjectionManager_ResumeFromCheckpoint_ReplaysFromStore 验证 ProjectionManager ResumeFromCheckpoint ReplaysFromStore。
func TestProjectionManager_ResumeFromCheckpoint_ReplaysFromStore(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)
	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	require.NoError(t, manager.RegisterProjection(projection))

	events := []eventing.Event[int64]{
		*eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, map[string]any{"i": 1}),
		*eventing.NewEvent[int64](1, "Agg", "TestEvent", 2, map[string]any{"i": 2}),
		*eventing.NewEvent[int64](1, "Agg", "TestEvent", 3, map[string]any{"i": 3}),
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

	status, err := manager.ProjectionStatus("test-projection")
	require.NoError(t, err)
	assert.Equal(t, int64(3), status.ProcessedEvents)
	assert.Equal(t, events[2].ID, status.LastEventID)
}

// TestProjectionManager_ResumeFromCheckpoint_ReplayFailureStops 验证 ProjectionManager ResumeFromCheckpoint ReplayFailureStops。
func TestProjectionManager_ResumeFromCheckpoint_ReplayFailureStops(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)
	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	// 处理时故意失败
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		return fmt.Errorf("fail:%s", event.GetID())
	}
	require.NoError(t, manager.RegisterProjection(projection))

	e1 := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	e2 := eventing.NewEvent[int64](1, "Agg", "TestEvent", 2, nil)
	now := time.Now()
	e1.Timestamp = now
	e2.Timestamp = now
	require.NoError(t, eventStore.AppendEvents(ctx, 1, []eventing.IStorableEvent[int64]{e1, e2}, 0))

	// checkpoint 在第一个事件
	checkpoint := NewCheckpoint("test-projection", 1, e1.ID, e1.Timestamp)
	require.NoError(t, checkpointStore.Save(ctx, checkpoint))

	err = manager.ResumeFromCheckpoint(ctx, "test-projection")
	require.Error(t, err)

	status, sErr := manager.ProjectionStatus("test-projection")
	require.NoError(t, sErr)
	assert.Equal(t, "error", status.Status)
	assert.Equal(t, int64(1), status.ProcessedEvents) // 未推进
	assert.Contains(t, status.LastError, "fail")
}

// TestProjectionManager_ResumeFromCheckpoint_CheckpointGap_FailFast 验证 ProjectionManager ResumeFromCheckpoint CheckpointGap FailFast。
func TestProjectionManager_ResumeFromCheckpoint_CheckpointGap_FailFast(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	eventStore := &cursorGapEventStore{
		events: []eventing.Event[int64]{
			{Message: messaging.Message{ID: "001", Type: "TestEvent", Timestamp: now, Metadata: messaging.NewMetadata()}},
			{Message: messaging.Message{ID: "002", Type: "TestEvent", Timestamp: now, Metadata: messaging.NewMetadata()}},
			{Message: messaging.Message{ID: "003", Type: "TestEvent", Timestamp: now, Metadata: messaging.NewMetadata()}},
		},
	}
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()

	cfg := &ProjectionConfig{}
	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)

	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	var rebuilt bool
	projection.rebuildFunc = func(ctx context.Context, events []eventing.Event[int64]) error {
		rebuilt = true
		return fmt.Errorf("unexpected rebuild")
	}
	require.NoError(t, manager.RegisterProjection(projection))

	// checkpoint 引用的 last_event_id 不存在（模拟事件被清理）
	require.NoError(t, checkpointStore.Save(ctx, NewCheckpoint("test-projection", 123, "missing", now)))

	err = manager.ResumeFromCheckpoint(ctx, "test-projection")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.NotFound))
	assert.False(t, rebuilt)
}

// TestProjectionManager_ReplayRetry_Success 验证 ProjectionManager ReplayRetry Success。
func TestProjectionManager_ReplayRetry_Success(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	cfg := &ProjectionConfig{
		MaxRetries:   2, // 允许最多重试 2 次（总尝试 <=3）
		RetryBackoff: 0 * time.Millisecond,
	}
	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	require.NoError(t, err)

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
	rt, ok := manager.runtime("retry-projection")
	require.True(t, ok)

	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	err = manager.applyReplayEvent(ctx, rt, evt)
	require.NoError(t, err)

	// 第一次失败 + 第二次成功
	assert.Equal(t, 2, attempts)

	status, sErr := manager.ProjectionStatus("retry-projection")
	require.NoError(t, sErr)
	assert.Equal(t, int64(1), status.ProcessedEvents)
	assert.Equal(t, int64(0), status.FailedEvents)
	assert.Equal(t, evt.ID, status.LastEventID)
}

// TestProjectionManager_ReplayRetry_MaxRetriesExceeded 验证 ProjectionManager ReplayRetry MaxRetriesExceeded。
func TestProjectionManager_ReplayRetry_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	cfg := &ProjectionConfig{
		MaxRetries:   2, // 最多重试 2 次（总尝试 3 次）
		RetryBackoff: 0 * time.Millisecond,
	}
	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	require.NoError(t, err)

	var attempts int
	projection := NewMockProjection("retry-projection", []string{"TestEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		attempts++
		return fmt.Errorf("always-fail:%s", event.GetID())
	}
	require.NoError(t, manager.RegisterProjection(projection))
	rt, ok := manager.runtime("retry-projection")
	require.True(t, ok)

	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	err = manager.applyReplayEvent(ctx, rt, evt)
	require.Error(t, err)

	// 初始 + 2 次重试，共 3 次
	assert.Equal(t, 3, attempts)

	status, sErr := manager.ProjectionStatus("retry-projection")
	require.NoError(t, sErr)
	// 重放失败时不会推进 ProcessedEvents，只统计 FailedEvents
	assert.Equal(t, int64(0), status.ProcessedEvents)
	assert.Equal(t, int64(1), status.FailedEvents)
	assert.Contains(t, status.LastError, "always-fail")
}

// TestProjectionManager_ReplayRetry_ContextCanceled 验证 ProjectionManager ReplayRetry ContextCanceled。
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
	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	require.NoError(t, err)

	var attempts int
	projection := NewMockProjection("retry-projection", []string{"TestEvent"})
	projection.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		attempts++
		return fmt.Errorf("cancelled:%s", event.GetID())
	}
	require.NoError(t, manager.RegisterProjection(projection))
	rt, ok := manager.runtime("retry-projection")
	require.True(t, ok)

	evt := eventing.NewEvent(1, "Agg", "TestEvent", 1, nil)
	err = manager.applyReplayEvent(ctx, rt, evt)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// 上下文在首次失败后即取消，不会进入下一轮重试
	assert.Equal(t, 1, attempts)

	status, sErr := manager.ProjectionStatus("retry-projection")
	require.NoError(t, sErr)
	// 因为在状态更新前即返回，Processed/Failed 都不应被修改
	assert.Equal(t, int64(0), status.ProcessedEvents)
	assert.Equal(t, int64(0), status.FailedEvents)
}

func TestProjectionManager_ApplyReplayEvent_UsesCheckpointingProjection(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()
	txRunner := &checkpointingTxRunner{}

	cfg := &ProjectionConfig{
		MaxRetries:             0,
		RetryBackoff:           0,
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    1,
	}
	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)

	projector := NewMockProjection("checkpointing-projection", []string{"TestEvent"})
	var saveSawTx bool
	projector.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		saveSawTx = ctx.Value(checkpointingTxKey{}) == true
		return nil
	}
	wrapped, err := NewCheckpointingProjector[int64](projector, txRunner)
	require.NoError(t, err)
	require.NoError(t, manager.RegisterProjection(wrapped))
	rt, ok := manager.runtime(wrapped.Name())
	require.True(t, ok)

	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	err = manager.applyReplayEvent(ctx, rt, evt)
	require.NoError(t, err)

	require.Equal(t, 1, txRunner.calls)
	require.True(t, saveSawTx)

	checkpoint, err := checkpointStore.Load(ctx, projector.Name())
	require.NoError(t, err)
	require.Equal(t, int64(1), checkpoint.Position)
	require.Equal(t, evt.ID, checkpoint.LastEventID)
}

func TestProjectionManager_ApplyReplayEvent_TenantAwareSkipSavesCheckpoint(t *testing.T) {
	ctx, err := contextx.WithTenantID(context.Background(), "tenant-a")
	require.NoError(t, err)

	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()
	txRunner := &checkpointingTxRunner{}

	cfg := &ProjectionConfig{
		MaxRetries:             0,
		RetryBackoff:           0,
		CheckpointSaveInterval: 0,
		CheckpointSaveCount:    1,
	}
	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, cfg)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)

	projector := NewMockProjection("tenant-skip-checkpointing-projection", []string{"TestEvent"})
	projector.handleFunc = func(ctx context.Context, event eventing.IEvent) error {
		t.Fatalf("tenant-mismatched event should not reach inner projection")
		return nil
	}
	wrapped, err := NewCheckpointingProjector[int64](projector, txRunner)
	require.NoError(t, err)
	tenantWrapped := NewTenantAwareProjector[int64](wrapped)
	require.NoError(t, manager.RegisterProjection(tenantWrapped))
	rt, ok := manager.runtime(tenantWrapped.Name())
	require.True(t, ok)

	evt := eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil)
	evt.GetMetadata().Set(contextx.MetadataTenantKey, "tenant-b")

	err = manager.applyReplayEvent(ctx, rt, evt)
	require.NoError(t, err)
	require.Equal(t, 0, txRunner.calls)

	checkpoint, err := checkpointStore.Load(ctx, tenantWrapped.Name())
	require.NoError(t, err)
	require.Equal(t, int64(1), checkpoint.Position)
	require.Equal(t, evt.ID, checkpoint.LastEventID)
}

func TestProjectionManager_RegisterTenantAwareProjectionRequiresCheckpointCapableInner(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)

	projection := NewTenantAwareProjector[int64](&nonCheckpointProjection{name: "tenant-plain-projection"})
	err = manager.RegisterProjectionWithContext(ctx, projection)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Unsupported))
}

func TestProjectionManager_WithCheckpointStoreRejectsExistingTenantAwareProjectionWithoutCheckpointInner(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)

	projection := NewTenantAwareProjector[int64](&nonCheckpointProjection{name: "tenant-existing-plain-projection"})
	require.NoError(t, manager.RegisterProjectionWithContext(ctx, projection))

	_, err = manager.WithCheckpointStore(NewMemoryCheckpointStore())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Unsupported))
}

func TestProjectionManager_RegisterProjectionRejectsMissingCheckpointRebuildCapability(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)

	projection := &rebuildMissingCheckpointProjection{
		inner: NewMockProjection("missing-rebuild-checkpoint-projection", []string{"TestEvent"}),
	}

	err = manager.RegisterProjectionWithContext(ctx, projection)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Unsupported))
	require.Contains(t, err.Error(), "checkpoint rebuild")
}

func TestProjectionManager_WithCheckpointStoreRejectsExistingProjectionMissingCheckpointRebuildCapability(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)

	projection := &rebuildMissingCheckpointProjection{
		inner: NewMockProjection("existing-missing-rebuild-checkpoint-projection", []string{"TestEvent"}),
	}
	require.NoError(t, manager.RegisterProjectionWithContext(ctx, projection))

	_, err = manager.WithCheckpointStore(NewMemoryCheckpointStore())
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.Unsupported))
	require.Contains(t, err.Error(), "checkpoint rebuild")
}

func TestProjectionManager_RebuildProjection_UsesCheckpointingProjector(t *testing.T) {
	ctx := context.Background()
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()
	txRunner := &checkpointingTxRunner{}

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)

	projector := NewMockProjection("rebuild-checkpointing-projection", []string{"TestEvent"})
	var rebuildSawTx bool
	projector.rebuildFunc = func(ctx context.Context, events []eventing.Event[int64]) error {
		rebuildSawTx = ctx.Value(checkpointingTxKey{}) == true
		return nil
	}
	wrapped, err := NewCheckpointingProjector[int64](projector, txRunner)
	require.NoError(t, err)
	require.NoError(t, manager.RegisterProjection(wrapped))

	events := []eventing.Event[int64]{
		*eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil),
		*eventing.NewEvent[int64](1, "Agg", "TestEvent", 2, nil),
	}

	require.NoError(t, manager.RebuildProjection(ctx, wrapped.Name(), events))
	require.Equal(t, 1, txRunner.calls)
	require.True(t, rebuildSawTx)

	checkpoint, err := checkpointStore.Load(ctx, wrapped.Name())
	require.NoError(t, err)
	require.Equal(t, int64(len(events)), checkpoint.Position)
	require.Equal(t, events[len(events)-1].ID, checkpoint.LastEventID)
}

func TestProjectionManager_RebuildProjection_TenantAwareProjectorPreservesCheckpointingRebuild(t *testing.T) {
	ctx, err := contextx.WithTenantID(context.Background(), "tenant-a")
	require.NoError(t, err)

	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}
	checkpointStore := NewMemoryCheckpointStore()
	txRunner := &checkpointingTxRunner{}

	reg := newTestRegistry(t)
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	require.NoError(t, err)
	manager, err = manager.WithCheckpointStore(checkpointStore)
	require.NoError(t, err)

	projector := NewMockProjection("tenant-rebuild-checkpointing-projection", []string{"TestEvent"})
	var rebuildSawTx bool
	var rebuiltEvents int
	projector.rebuildFunc = func(ctx context.Context, events []eventing.Event[int64]) error {
		rebuildSawTx = ctx.Value(checkpointingTxKey{}) == true
		rebuiltEvents = len(events)
		return nil
	}
	wrapped, err := NewCheckpointingProjector[int64](projector, txRunner)
	require.NoError(t, err)
	tenantWrapped := NewTenantAwareProjector[int64](wrapped)
	require.NoError(t, manager.RegisterProjection(tenantWrapped))

	events := []eventing.Event[int64]{
		*eventing.NewEvent[int64](1, "Agg", "TestEvent", 1, nil),
		*eventing.NewEvent[int64](1, "Agg", "TestEvent", 2, nil),
	}
	events[0].GetMetadata().Set(contextx.MetadataTenantKey, "tenant-a")
	events[1].GetMetadata().Set(contextx.MetadataTenantKey, "tenant-b")

	require.NoError(t, manager.RebuildProjection(ctx, tenantWrapped.Name(), events))
	require.Equal(t, 1, txRunner.calls)
	require.True(t, rebuildSawTx)
	require.Equal(t, 1, rebuiltEvents)

	checkpoint, err := checkpointStore.Load(ctx, tenantWrapped.Name())
	require.NoError(t, err)
	require.Equal(t, int64(len(events)), checkpoint.Position)
	require.Equal(t, events[len(events)-1].ID, checkpoint.LastEventID)
}
