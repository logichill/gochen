package projection

import (
	"context"
	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/eventing"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/messaging"
)

// TestProjectionEventHandler_ShouldProcessOnlyWhenRunning 验证 ProjectionEventHandler ShouldProcessOnlyWhenRunning。
func TestProjectionEventHandler_ShouldProcessOnlyWhenRunning(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("TestEvent", func() any { return &struct{}{} }))
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	projection := NewMockProjection("test-projection", []string{"TestEvent"})
	err = manager.RegisterProjection(projection)
	assert.NoError(t, err)

	rt, ok := manager.runtime(projection.Name())
	require.True(t, ok)
	handler := rt.handlers["TestEvent"]
	require.NotNil(t, handler)

	evt := &eventing.Event[int64]{
		Message: messaging.Message{
			ID:        "event-1",
			Type:      "TestEvent",
			Timestamp: time.Now(),
			Metadata:  messaging.NewMetadata(),
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

func TestProjectionEventHandler_WithoutEventStore_DoesNotSwallowEvents(t *testing.T) {
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	require.NoError(t, reg.Register("TestEvent", func() any { return &struct{}{} }))
	upgraders := upcast.NewUpgraderRegistry()
	// Manager without eventStore
	manager, err := NewProjectionManager[int64](nil, eventBus, reg, upgraders)
	assert.NoError(t, err)
	// Artificially attach checkpointStore to simulate configuration
	manager.checkpointStore = NewMemoryCheckpointStore()

	projection := NewMockProjection("test-projection-no-store", []string{"TestEvent"})
	err = manager.RegisterProjection(projection)
	assert.NoError(t, err)

	rt, ok := manager.runtime(projection.Name())
	require.True(t, ok)
	handler := rt.handlers["TestEvent"]
	require.NotNil(t, handler)

	err = manager.StartProjection("test-projection-no-store")
	assert.NoError(t, err)

	evt := &eventing.Event[int64]{
		Message: messaging.Message{
			ID:        "event-no-store-1",
			Type:      "TestEvent",
			Timestamp: time.Now(),
			Metadata:  messaging.NewMetadata(),
		},
	}

	err = handler.HandleEvent(context.Background(), evt)
	assert.NoError(t, err)
	assert.Equal(t, 1, projection.processedEvents)
}
