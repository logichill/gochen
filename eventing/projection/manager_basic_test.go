package projection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gochen/errors"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
)

// TestNewProjectionManager 验证 NewProjectionManager。
func TestNewProjectionManager(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	// EventBus requires transport, use simple mock for now
	eventBus := &MockEventBus{}

	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManager[int64](eventStore, eventBus, reg, upgraders)
	assert.NoError(t, err)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.runtimes)
	assert.NotNil(t, manager.config)
}

func TestNewProjectionManagerRejectsNilEventBus(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()

	manager, err := NewProjectionManager[int64](eventStore, nil, reg, upgraders)

	assert.Nil(t, manager)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got: %v", err)
	assert.Contains(t, err.Error(), "event bus")
}

// TestProjectionManager_CustomConfig 验证 ProjectionManager CustomConfig。
func TestProjectionManager_CustomConfig(t *testing.T) {
	eventStore := store.NewMemoryEventStore()
	eventBus := &MockEventBus{}

	config := &ProjectionConfig{
		MaxRetries:   5,
		RetryBackoff: 2 * time.Second,
	}

	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, config)
	assert.NoError(t, err)

	assert.NotNil(t, manager)
	assert.Equal(t, 5, manager.config.MaxRetries)
	assert.Equal(t, 2*time.Second, manager.config.RetryBackoff)
}

// TestDefaultProjectionConfig 验证 DefaultProjectionConfig。
func TestDefaultProjectionConfig(t *testing.T) {
	config := DefaultProjectionConfig()

	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.RetryBackoff)
	assert.NotNil(t, config.DeadLetterFunc)
}
