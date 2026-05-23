package outbox

import (
	"testing"
	"time"

	"gochen/errors"

	"github.com/stretchr/testify/assert"
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
	normalized := normalizeOutboxConfig(cfg)
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
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewPublisher_RejectsNilEventBus(t *testing.T) {
	publisher, err := NewPublisher[int64](&MockOutboxRepository{}, nil, OutboxConfig{}, nil, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewParallelPublisher_RejectsNilRepository(t *testing.T) {
	publisher, err := NewParallelPublisher[int64](nil, &MockEventBus{}, OutboxConfig{}, nil, 2, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewParallelPublisher_RejectsNilEventBus(t *testing.T) {
	publisher, err := NewParallelPublisher[int64](&MockOutboxRepository{}, nil, OutboxConfig{}, nil, 2, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewPublisher_RejectsClaimLeaseMismatch(t *testing.T) {
	repo := &leaseAwareMockRepo{claimLease: 2 * time.Minute}
	cfg := OutboxConfig{ClaimLease: time.Minute}

	publisher, err := NewPublisher[int64](repo, &MockEventBus{}, cfg, nil, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
}

func TestNewParallelPublisher_RejectsClaimLeaseMismatch(t *testing.T) {
	repo := &leaseAwareMockRepo{claimLease: 2 * time.Minute}
	cfg := OutboxConfig{ClaimLease: time.Minute}

	publisher, err := NewParallelPublisher[int64](repo, &MockEventBus{}, cfg, nil, 2, newTestRegistry(t), newTestUpgraders())

	assert.Nil(t, publisher)
	assert.True(t, errors.Is(err, errors.InvalidInput), "expected InvalidInput, got %v", err)
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
