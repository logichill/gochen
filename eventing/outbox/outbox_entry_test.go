package outbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gochen/eventing/registry"
	"gochen/eventing/upcast"
)

// TestOutboxEntry_TableName 验证 OutboxEntry TableName。
func TestOutboxEntry_TableName(t *testing.T) {
	entry := OutboxEntry[int64]{}
	assert.Equal(t, "event_outbox", entry.TableName())
}

// TestOutboxEntry_ToEvent 验证 OutboxEntry ToEvent。
func TestOutboxEntry_ToEvent(t *testing.T) {
	evt := newTestEvent(123, 1, "event-1", map[string]any{"value": 100})
	entry, err := EventToOutboxEntry(123, evt)
	require.NoError(t, err)

	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	decoded, err := entry.ToEventWith(reg, upgraders)
	assert.NoError(t, err)
	assert.Equal(t, evt.ID, decoded.ID)
	assert.Equal(t, evt.Type, decoded.Type)
	assert.Equal(t, evt.AggregateID, decoded.AggregateID)
}

// TestOutboxEntry_ToEvent_InvalidData 验证 OutboxEntry ToEvent InvalidData。
func TestOutboxEntry_ToEvent_InvalidData(t *testing.T) {
	entry := &OutboxEntry[int64]{
		EventData: "invalid json {{{",
	}
	reg := registry.NewRegistry()
	upgraders := upcast.NewUpgraderRegistry()
	_, err := entry.ToEventWith(reg, upgraders)
	assert.Error(t, err)
}

// TestOutboxEntry_ShouldRetry 验证 OutboxEntry ShouldRetry。
func TestOutboxEntry_ShouldRetry(t *testing.T) {
	maxRetries := 5

	t.Run("应该重试 - 失败且未超过最大次数", func(t *testing.T) {
		now := time.Now()
		entry := &OutboxEntry[int64]{
			Status:      OutboxStatusFailed,
			RetryCount:  2,
			NextRetryAt: &now,
		}
		time.Sleep(1 * time.Millisecond)
		assert.True(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("应该重试 - NextRetryAt 为空", func(t *testing.T) {
		entry := &OutboxEntry[int64]{
			Status:      OutboxStatusFailed,
			RetryCount:  2,
			NextRetryAt: nil,
		}
		assert.True(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("不应该重试 - 状态不是失败", func(t *testing.T) {
		entry := &OutboxEntry[int64]{
			Status:     OutboxStatusPending,
			RetryCount: 2,
		}
		assert.False(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("不应该重试 - 超过最大重试次数", func(t *testing.T) {
		entry := &OutboxEntry[int64]{
			Status:     OutboxStatusFailed,
			RetryCount: 6,
		}
		assert.False(t, entry.ShouldRetry(maxRetries))
	})

	t.Run("不应该重试 - 未到重试时间", func(t *testing.T) {
		future := time.Now().Add(1 * time.Hour)
		entry := &OutboxEntry[int64]{
			Status:      OutboxStatusFailed,
			RetryCount:  2,
			NextRetryAt: &future,
		}
		assert.False(t, entry.ShouldRetry(maxRetries))
	})
}

// TestOutboxEntry_CalculateNextRetryTime 验证 OutboxEntry CalculateNextRetryTime。
func TestOutboxEntry_CalculateNextRetryTime(t *testing.T) {
	baseInterval := 30 * time.Second

	t.Run("第一次重试", func(t *testing.T) {
		entry := &OutboxEntry[int64]{RetryCount: 0}
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		assert.WithinDuration(t, time.Now().Add(30*time.Second), nextTime, 1*time.Second)
	})

	t.Run("第二次重试", func(t *testing.T) {
		entry := &OutboxEntry[int64]{RetryCount: 1}
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		assert.WithinDuration(t, time.Now().Add(60*time.Second), nextTime, 1*time.Second)
	})

	t.Run("第三次重试", func(t *testing.T) {
		entry := &OutboxEntry[int64]{RetryCount: 2}
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		assert.WithinDuration(t, time.Now().Add(120*time.Second), nextTime, 1*time.Second)
	})

	t.Run("最大退避限制", func(t *testing.T) {
		entry := &OutboxEntry[int64]{RetryCount: 10}
		nextTime := entry.CalculateNextRetryTime(baseInterval)
		assert.WithinDuration(t, time.Now().Add(32*30*time.Second), nextTime, 1*time.Second)
	})
}

// TestEventToOutboxEntry 验证 EventToOutboxEntry。
func TestEventToOutboxEntry(t *testing.T) {
	evt := newTestEvent(123, 1, "event-1", map[string]any{
		"user":  "alice",
		"value": 100,
	})

	entry, err := EventToOutboxEntry(123, evt)
	assert.NoError(t, err)
	assert.NotNil(t, entry)
	assert.Equal(t, int64(123), entry.AggregateID)
	assert.Equal(t, "TestAggregate", entry.AggregateType)
	assert.Equal(t, "event-1", entry.EventID)
	assert.Equal(t, "TestEvent", entry.EventType)
	assert.Equal(t, OutboxStatusPending, entry.Status)
	assert.NotEmpty(t, entry.EventData)
}
