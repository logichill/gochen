package outbox

import (
	"context"
	"gochen/errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gochen/eventing"
	"gochen/logging"
)

type recordingPublisherMetrics struct {
	mu sync.Mutex

	decodeCount  int
	decodeErrors int

	publishCount  int
	publishErrors int
}

// RecordOutboxDecode err：待检查/包装的错误（类型：bool）。
//
// 参数：
func (r *recordingPublisherMetrics) RecordOutboxDecode(_ time.Duration, err bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decodeCount++
	if err {
		r.decodeErrors++
	}
}

// RecordOutboxPublish err：待检查/包装的错误（类型：bool）。
//
// 参数：
func (r *recordingPublisherMetrics) RecordOutboxPublish(_ time.Duration, err bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publishCount++
	if err {
		r.publishErrors++
	}
}

// TestPublisher_MetricsRecorder_IsCalled 验证 Publisher MetricsRecorder IsCalled。
func TestPublisher_MetricsRecorder_IsCalled(t *testing.T) {
	repo := &MockOutboxRepository{
		entries: []OutboxEntry[int64]{
			{
				ID:        1,
				EventType: "TestEvent",
				EventData: "invalid json {{{",
				Status:    OutboxStatusPending,
				CreatedAt: time.Now(),
			},
		},
	}
	eventBus := &MockEventBus{}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
	}

	evt := newTestEvent(1, 1, "event-1", map[string]any{"value": 123})
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))

	rec := &recordingPublisherMetrics{}
	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	require.NoError(t, err)
	publisher.SetMetricsRecorder(rec)

	require.NoError(t, publisher.PublishPending(context.Background()))

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Equal(t, 2, rec.decodeCount)
	require.Equal(t, 1, rec.decodeErrors)
	require.Equal(t, 1, rec.publishCount)
	require.Equal(t, 0, rec.publishErrors)
}

// TestPublisher_MetricsRecorder_RecordsPublishError 验证 Publisher MetricsRecorder RecordsPublishError。
func TestPublisher_MetricsRecorder_RecordsPublishError(t *testing.T) {
	repo := &MockOutboxRepository{}
	eventBus := &MockEventBus{publishError: errors.New("boom")}
	cfg := OutboxConfig{
		BatchSize:     10,
		RetryInterval: 30 * time.Second,
		MaxRetries:    1,
	}

	evt := newTestEvent(1, 1, "event-1", map[string]any{"value": 123})
	require.NoError(t, repo.SaveWithEvents(context.Background(), 1, []eventing.Event[int64]{evt}))

	rec := &recordingPublisherMetrics{}
	reg := newTestRegistry(t)
	upgraders := newTestUpgraders()
	publisher, err := NewPublisher(repo, eventBus, cfg, logging.NewNoopLogger(), reg, upgraders)
	require.NoError(t, err)
	publisher.SetMetricsRecorder(rec)

	require.NoError(t, publisher.PublishPending(context.Background()))

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Equal(t, 1, rec.decodeCount)
	require.Equal(t, 0, rec.decodeErrors)
	require.Equal(t, 1, rec.publishCount)
	require.Equal(t, 1, rec.publishErrors)
}
