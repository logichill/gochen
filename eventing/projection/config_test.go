package projection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

// TestPresetProjectionConfigs 验证 PresetProjectionConfigs。
func TestPresetProjectionConfigs(t *testing.T) {
	lowLatency := ProjectionConfigPresets.LowLatency()
	require.Equal(t, 1*time.Second, lowLatency.CheckpointSaveInterval)
	require.Equal(t, 50, lowLatency.CheckpointSaveCount)

	highThroughput := ProjectionConfigPresets.HighThroughput()
	require.Equal(t, 15*time.Second, highThroughput.CheckpointSaveInterval)
	require.Equal(t, 1000, highThroughput.CheckpointSaveCount)
}
