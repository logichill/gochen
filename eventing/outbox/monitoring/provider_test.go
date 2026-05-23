package monitoring_test

import (
	"context"
	"testing"
	"time"

	coremon "gochen/eventing/monitoring"
	"gochen/eventing/outbox"
	outboxmon "gochen/eventing/outbox/monitoring"
)

type fakeCollector struct {
	metrics *outbox.OutboxMetrics
	health  outbox.HealthStatus
	msg     string
}

// Collect ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - result1：返回结果（类型：*outbox.OutboxMetrics）
// - err：错误信息（nil 表示成功）
func (f *fakeCollector) Collect(ctx context.Context) (*outbox.OutboxMetrics, error) {
	return f.metrics, nil
}

// HealthStatus 返回当前值。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：返回结果（类型：outbox.HealthStatus）
// - result2：文本结果
// - err：错误信息（nil 表示成功）
func (f *fakeCollector) HealthStatus(ctx context.Context) (outbox.HealthStatus, string, error) {
	return f.health, f.msg, nil
}

// TestProviderSnapshotAndHealth 验证 ProviderSnapshotAndHealth。
func TestProviderSnapshotAndHealth(t *testing.T) {
	t.Parallel()

	collector := &fakeCollector{
		metrics: &outbox.OutboxMetrics{
			PendingCount:     10,
			PublishedCount:   20,
			FailedCount:      3,
			DLQCount:         1,
			MaxRetryCount:    7,
			AvgRetryCount:    2.5,
			HighRetryCount:   4,
			OldestPendingAge: 15 * time.Second,
			AvgPublishDelay:  200 * time.Millisecond,
			CollectedAt:      time.Unix(123, 0).UTC(),
		},
		health: outbox.HealthStatusDegraded,
		msg:    "degraded",
	}

	p, err := outboxmon.NewProvider(collector)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}

	ctx := context.Background()
	status, msg, err := p.Health(ctx)
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if status != coremon.HealthStatusDegraded {
		t.Fatalf("unexpected status: %v", status)
	}
	if msg != "degraded" {
		t.Fatalf("unexpected msg: %q", msg)
	}

	snap, err := p.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() error: %v", err)
	}
	if snap.Health != coremon.HealthStatusDegraded {
		t.Fatalf("unexpected snapshot health: %v", snap.Health)
	}
	if snap.Metrics == nil {
		t.Fatalf("Metrics should not be nil")
	}
	// JSON map 反序列化后数值会变为 float64；这里只校验关键字段存在即可，避免与 JSON 细节耦合。
	for _, k := range []string{
		"pending_count",
		"published_count",
		"failed_count",
		"dlq_count",
		"max_retry_count",
		"avg_retry_count",
		"high_retry_count",
		"oldest_pending_age",
		"avg_publish_delay",
		"collected_at",
	} {
		if _, ok := snap.Metrics[k]; !ok {
			t.Fatalf("missing metrics key: %s", k)
		}
	}
}

// TestNewProviderReturnsErrorOnNilCollector 验证 NewProviderReturnsErrorOnNilCollector。
func TestNewProviderReturnsErrorOnNilCollector(t *testing.T) {
	t.Parallel()

	if _, err := outboxmon.NewProvider(nil); err == nil {
		t.Fatalf("expected error")
	}
}
