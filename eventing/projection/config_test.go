package projection

import (
	"sync"
	"testing"
	"time"

	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/testkit/require"
)

// TestNormalizeProjectionConfig_Defaults 验证 NormalizeProjectionConfig Defaults。
func TestNormalizeProjectionConfig_Defaults(t *testing.T) {
	cfg := normalizeProjectionConfig(nil)
	require.NotNil(t, cfg)
	require.Equal(t, 3, cfg.MaxRetries)
	require.Equal(t, 1*time.Second, cfg.RetryBackoff)
	require.Equal(t, 5*time.Second, cfg.CheckpointSaveInterval)
	require.Equal(t, 100, cfg.CheckpointSaveCount)
	require.NotNil(t, cfg.DeadLetterFunc)
}

// TestNormalizeProjectionConfig_NegativeCheckpointFallsBackToDefaults 验证 NormalizeProjectionConfig NegativeCheckpointFallsBackToDefaults。
func TestNormalizeProjectionConfig_NegativeCheckpointFallsBackToDefaults(t *testing.T) {
	cfg := normalizeProjectionConfig(&ProjectionConfig{
		CheckpointSaveInterval: -1 * time.Second,
		CheckpointSaveCount:    -100,
	})
	require.Equal(t, 5*time.Second, cfg.CheckpointSaveInterval)
	require.Equal(t, 100, cfg.CheckpointSaveCount)
}

// TestNormalizeProjectionConfig_NegativeRetriesClamped 验证 NormalizeProjectionConfig NegativeRetriesClamped。
func TestNormalizeProjectionConfig_NegativeRetriesClamped(t *testing.T) {
	cfg := normalizeProjectionConfig(&ProjectionConfig{
		MaxRetries:   -1,
		RetryBackoff: -1 * time.Second,
	})
	require.Equal(t, 0, cfg.MaxRetries)
	require.Equal(t, time.Duration(0), cfg.RetryBackoff)
}

// TestNormalizeProjectionConfig_DoesNotMutateInput 验证归一化只写副本，不回写调用方配置。
func TestNormalizeProjectionConfig_DoesNotMutateInput(t *testing.T) {
	input := &ProjectionConfig{
		MaxRetries:             -1,
		RetryBackoff:           -1 * time.Second,
		CheckpointSaveInterval: -1 * time.Second,
		CheckpointSaveCount:    -100,
	}

	normalized := normalizeProjectionConfig(input)

	require.NotSame(t, input, normalized)
	require.Equal(t, 0, normalized.MaxRetries)
	require.Equal(t, time.Duration(0), normalized.RetryBackoff)
	require.Equal(t, 5*time.Second, normalized.CheckpointSaveInterval)
	require.Equal(t, 100, normalized.CheckpointSaveCount)
	require.NotNil(t, normalized.DeadLetterFunc)

	require.Equal(t, -1, input.MaxRetries)
	require.Equal(t, -1*time.Second, input.RetryBackoff)
	require.Equal(t, -1*time.Second, input.CheckpointSaveInterval)
	require.Equal(t, -100, input.CheckpointSaveCount)
	require.Nil(t, input.DeadLetterFunc)
}

// TestNewProjectionManagerWithConfig_ConcurrentSharedInputKeepsConfigImmutable 验证多个构造
// 并发共享同一份调用方配置时不产生数据竞争、也不回写输入（需配合 -race 运行）。
func TestNewProjectionManagerWithConfig_ConcurrentSharedInputKeepsConfigImmutable(t *testing.T) {
	eventStore := store.NewMemoryEventStore[int64]()
	eventBus := &MockEventBus{}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	input := &ProjectionConfig{MaxRetries: -1, RetryBackoff: -1 * time.Second}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			manager, err := NewProjectionManagerWithConfig[int64](eventStore, eventBus, reg, upgraders, input)
			if err != nil || manager == nil {
				t.Errorf("concurrent NewProjectionManagerWithConfig failed: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, -1, input.MaxRetries)
	require.Equal(t, -1*time.Second, input.RetryBackoff)
	require.Nil(t, input.DeadLetterFunc)
}

// TestPresetProjectionConfigs 验证 PresetProjectionConfigs。
func TestPresetProjectionConfigs(t *testing.T) {
	lowLatency := ProjectionConfigPresets.LowLatency()
	require.Equal(t, 1*time.Second, lowLatency.CheckpointSaveInterval)
	require.Equal(t, 50, lowLatency.CheckpointSaveCount)

	highThroughput := ProjectionConfigPresets.HighThroughput()
	require.Equal(t, 15*time.Second, highThroughput.CheckpointSaveInterval)
	require.Equal(t, 1000, highThroughput.CheckpointSaveCount)
}
