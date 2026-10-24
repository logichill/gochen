package outbox

import (
	"context"
	"testing"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/observe/logging"

	"gochen/testkit/assert"
	"gochen/testkit/require"
)

// TestDefaultOutboxConfig 验证 DefaultOutboxConfig。
func TestDefaultOutboxConfig(t *testing.T) {
	cfg := DefaultOutboxConfig()
	assert.Equal(t, 5*time.Second, cfg.PublishInterval)
	assert.Equal(t, 100, cfg.BatchSize)
	assert.Equal(t, 5, cfg.MaxRetries)
	assert.Equal(t, 30*time.Second, cfg.RetryInterval)
	assert.Equal(t, 1*time.Hour, cfg.CleanupInterval)
	assert.Equal(t, 7*24*time.Hour, cfg.RetentionPeriod)
	assert.Equal(t, 5*time.Minute, cfg.ClaimLease)
	assert.Equal(t, 0*time.Second, cfg.ClaimRenewInterval)
	assert.Equal(t, 3, cfg.MarkPublishedAttempts)
	normalized := NormalizeOutboxConfig(cfg)
	assert.Equal(t, cfg.ClaimLease/2, normalized.ClaimRenewInterval)
}

type leaseAwareMockRepo struct {
	MockOutboxRepository
	claimLease time.Duration
}

func (r *leaseAwareMockRepo) GetClaimLease() time.Duration {
	return r.claimLease
}

func TestNewPublisher_AdoptsRepositoryClaimLeaseWhenConfigUnset(t *testing.T) {
	repo := &leaseAwareMockRepo{claimLease: 2 * time.Minute}
	publisher, err := NewPublisher[int64](repo, &MockEventBus{}, OutboxConfig{}, nil, newTestRegistry(t), newTestUpgraders())

	if !assert.NoError(t, err) || !assert.NotNil(t, publisher) {
		return
	}
	assert.Equal(t, 2*time.Minute, publisher.cfg.ClaimLease)
	assert.Equal(t, time.Minute, publisher.cfg.ClaimRenewInterval)
}

func TestNewPublisher_RejectsNilRepository(t *testing.T) {
	publisher, err := NewPublisher[int64](nil, &MockEventBus{}, OutboxConfig{}, nil, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewPublisher_RejectsNilEventBus(t *testing.T) {
	publisher, err := NewPublisher[int64](&MockOutboxRepository{}, nil, OutboxConfig{}, nil, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewParallelPublisher_RejectsNilRepository(t *testing.T) {
	publisher, err := NewParallelPublisher[int64](nil, &MockEventBus{}, OutboxConfig{}, nil, 2, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewParallelPublisher_RejectsNilEventBus(t *testing.T) {
	publisher, err := NewParallelPublisher[int64](&MockOutboxRepository{}, nil, OutboxConfig{}, nil, 2, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewPublisher_RejectsClaimLeaseMismatch(t *testing.T) {
	repo := &leaseAwareMockRepo{claimLease: 2 * time.Minute}
	cfg := OutboxConfig{ClaimLease: time.Minute}

	publisher, err := NewPublisher[int64](repo, &MockEventBus{}, cfg, nil, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewParallelPublisher_RejectsClaimLeaseMismatch(t *testing.T) {
	repo := &leaseAwareMockRepo{claimLease: 2 * time.Minute}
	cfg := OutboxConfig{ClaimLease: time.Minute}

	publisher, err := NewParallelPublisher[int64](repo, &MockEventBus{}, cfg, nil, 2, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.Truef(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewPublisher_AllowsExplicitRenewIntervalWithRepositoryLease(t *testing.T) {
	repo := &leaseAwareMockRepo{claimLease: 2 * time.Minute}
	cfg := OutboxConfig{ClaimRenewInterval: 30 * time.Second}

	publisher, err := NewPublisher[int64](repo, &MockEventBus{}, cfg, nil, newTestRegistry(t), newTestUpgraders())

	if !assert.NoError(t, err) || !assert.NotNil(t, publisher) {
		return
	}
	assert.Equal(t, 2*time.Minute, publisher.cfg.ClaimLease)
	assert.Equal(t, 30*time.Second, publisher.cfg.ClaimRenewInterval)
}

func TestNormalizeOutboxConfig_SupportsNoMaxRetries(t *testing.T) {
	// 显式指定 NoMaxRetries (-1)
	cfg := OutboxConfig{MaxRetries: NoMaxRetries}
	normalized := NormalizeOutboxConfig(cfg)
	assert.Equal(t, NoMaxRetries, normalized.MaxRetries)

	// 未指定 (0) 使用默认值 DefaultMaxRetries (5)
	cfgZero := OutboxConfig{MaxRetries: 0}
	normalizedZero := NormalizeOutboxConfig(cfgZero)
	assert.Equal(t, DefaultMaxRetries, normalizedZero.MaxRetries)
}

func TestNewPublisher_PropagatesNoMaxRetriesToMemoryRepository(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	cfg := DefaultOutboxConfig()
	cfg.MaxRetries = NoMaxRetries

	publisher, err := NewPublisher[int64](repo, &MockEventBus{}, cfg, nil, newTestRegistry(t), newTestUpgraders())
	if !assert.NoError(t, err) || !assert.NotNil(t, publisher) {
		return
	}
	assert.Equal(t, NoMaxRetries, publisher.cfg.MaxRetries)

	require.NoError(t, repo.SaveWithEvents(context.Background(), int64(1), []eventing.Event[int64]{
		newTestEvent(1, 1, "event-no-max-retries", nil),
	}))
	repo.mu.Lock()
	repo.entries[0].Status = OutboxStatusFailed
	repo.entries[0].RetryCount = DefaultMaxRetries * 2
	repo.entries[0].NextRetryAt = timePointer(time.Now().Add(-time.Hour))
	repo.mu.Unlock()

	claimed, err := repo.ClaimPendingEntries(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
}

func TestNewPublisher_AdoptsExplicitRepositoryMaxRetries(t *testing.T) {
	repo := newMemoryOutboxRepository(t)
	repo.WithMaxRetries(3)

	publisher, err := NewPublisher[int64](repo, &MockEventBus{}, DefaultOutboxConfig(), nil, newTestRegistry(t), newTestUpgraders())
	if !assert.NoError(t, err) || !assert.NotNil(t, publisher) {
		return
	}
	assert.Equal(t, 3, publisher.cfg.MaxRetries)
}

// TestNewPublisher_SecondPublisherAdoptsAppliedRepositoryMaxRetries 锁定 MaxRetries
// 协商语义（IOutboxMaxRetriesAware）：publisher 首次下发后仓储即视为已对齐，
// 后续以不同 MaxRetries 构造的 publisher 采纳仓储值而不是覆盖，避免多 publisher
// 共享仓储时 claim 门禁与 DLQ 判定错位。
func TestNewPublisher_SecondPublisherAdoptsAppliedRepositoryMaxRetries(t *testing.T) {
	repo := newMemoryOutboxRepository(t)

	cfg1 := DefaultOutboxConfig()
	cfg1.MaxRetries = 7
	_, err := NewPublisher(repo, &MockEventBus{}, cfg1, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)

	maxRetries, configured := repo.ConfiguredMaxRetries()
	require.Equal(t, 7, maxRetries)
	require.True(t, configured)

	cfg2 := DefaultOutboxConfig()
	cfg2.MaxRetries = 3
	p2, err := NewPublisher(repo, &MockEventBus{}, cfg2, logging.NewNoopLogger(), newTestRegistry(t), newTestUpgraders())
	require.NoError(t, err)
	require.Equal(t, 7, p2.cfg.MaxRetries)

	maxRetries, configured = repo.ConfiguredMaxRetries()
	require.Equal(t, 7, maxRetries)
	require.True(t, configured)
}
