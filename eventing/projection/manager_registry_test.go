package projection

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gochen/errors"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
)

// TestProjectionManager_RegisterProjection 验证 ProjectionManager RegisterProjection。
func TestProjectionManager_RegisterProjection(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
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
	eventStore := store.NewMemoryEventStore()
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
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got: %v", err)
	assert.Contains(t, err.Error(), "event bus")
}

// TestProjectionManager_GetProjectionStatus 验证 ProjectionManager ProjectionStatus。
func TestProjectionManager_GetProjectionStatus(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
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
	eventStore := store.NewMemoryEventStore()
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
	eventStore := store.NewMemoryEventStore()
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
	eventStore := store.NewMemoryEventStore()
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
	eventStore := store.NewMemoryEventStore()
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
	eventStore := store.NewMemoryEventStore()
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
