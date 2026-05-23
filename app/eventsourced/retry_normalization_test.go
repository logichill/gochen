package eventsourced

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/policy/retry"
)

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()

	require.NotNil(t, cfg)
	require.Equal(t, 3, cfg.MaxRetries)
	require.Equal(t, 10*time.Millisecond, cfg.InitialBackoff)
	require.Equal(t, time.Second, cfg.MaxBackoff)
	require.Equal(t, 2.0, cfg.BackoffMultiplier)
	require.Equal(t, 0.2, cfg.JitterRatio)
}

func TestNormalizeRetryConfig_DefaultsAndBounds(t *testing.T) {
	require.Nil(t, normalizeRetryConfig(nil))

	cfg := normalizeRetryConfig(&RetryConfig{
		MaxRetries:        -1,
		InitialBackoff:    -time.Millisecond,
		MaxBackoff:        -time.Second,
		BackoffMultiplier: 0,
		JitterRatio:       2,
	})

	require.Equal(t, 0, cfg.MaxRetries)
	require.Zero(t, cfg.InitialBackoff)
	require.Zero(t, cfg.MaxBackoff)
	require.Equal(t, 2.0, cfg.BackoffMultiplier)
	require.Equal(t, 1.0, cfg.JitterRatio)

	cfg = normalizeRetryConfig(&RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    5 * time.Millisecond,
		MaxBackoff:        0,
		BackoffMultiplier: -1,
		JitterRatio:       -1,
	})

	require.Equal(t, 2, cfg.MaxRetries)
	require.Equal(t, 5*time.Millisecond, cfg.InitialBackoff)
	require.Equal(t, 5*time.Millisecond, cfg.MaxBackoff)
	require.Equal(t, 2.0, cfg.BackoffMultiplier)
	require.Zero(t, cfg.JitterRatio)
}

func TestRetryConfigToPolicyConfig_DisabledWhenNil(t *testing.T) {
	var cfg *RetryConfig
	policyCfg := cfg.toPolicyConfig(DefaultIsConcurrencyError)

	require.Equal(t, 1, policyCfg.MaxAttempts)
	require.NotNil(t, policyCfg.RetryIf)
	require.False(t, policyCfg.RetryIf(errors.NewCode(errors.Concurrency, "conflict")))
}

func TestRetryConfigToPolicyConfig_MapsNormalizedConcurrencyPolicy(t *testing.T) {
	cfg := normalizeRetryConfig(&RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    5 * time.Millisecond,
		MaxBackoff:        0,
		BackoffMultiplier: 0,
		JitterRatio:       0,
	})

	policyCfg := cfg.toPolicyConfig(nil)

	require.Equal(t, 3, policyCfg.MaxAttempts)
	require.Equal(t, 5*time.Millisecond, policyCfg.InitialDelay)
	require.Equal(t, 2.0, policyCfg.BackoffFactor)
	require.Equal(t, 5*time.Millisecond, policyCfg.MaxDelay)
	require.Zero(t, policyCfg.JitterRatio)
	require.True(t, policyCfg.RetryIf(errors.NewCode(errors.Concurrency, "conflict")))
	require.False(t, policyCfg.RetryIf(errors.NewCode(errors.Validation, "invalid")))
	require.Equal(t, 5*time.Millisecond, retry.ComputeDelay(policyCfg, 2))
}

func TestRetryConfigToPolicyConfig_UsesCustomConcurrencyClassifier(t *testing.T) {
	sentinel := stderrors.New("custom conflict")
	cfg := normalizeRetryConfig(&RetryConfig{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})

	policyCfg := cfg.toPolicyConfig(func(err error) bool {
		return err == sentinel
	})

	require.True(t, policyCfg.RetryIf(sentinel))
	require.False(t, policyCfg.RetryIf(errors.NewCode(errors.Concurrency, "conflict")))
}
