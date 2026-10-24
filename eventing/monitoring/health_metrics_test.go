package monitoring

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMetricsHealthCheck_NilMetrics(t *testing.T) {
	if _, err := MetricsHealthCheck(nil, DefaultMetricsHealthConfig()); err == nil {
		t.Fatalf("expected error for nil metrics")
	}
}

func TestMetricsHealthCheck_Healthy(t *testing.T) {
	m := NewMetrics()
	m.RecordEventSaved(10, time.Millisecond)
	m.RecordProjectionUpdate(true, 100*time.Millisecond)

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, err := check(context.Background())
	if err != nil {
		t.Fatalf("unexpected check error: %v", err)
	}
	if status != HealthStatusHealthy {
		t.Fatalf("expected healthy, got %s (%s)", status, msg)
	}
}

func TestMetricsHealthCheck_EventStoreAllCallsFailed(t *testing.T) {
	m := NewMetrics()
	m.RecordEventStoreError()
	m.RecordEventStoreError()

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusUnhealthy {
		t.Fatalf("expected unhealthy, got %s (%s)", status, msg)
	}
	if !strings.Contains(msg, "all calls failed") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestMetricsHealthCheck_EventStoreDegraded(t *testing.T) {
	m := NewMetrics()
	// 9 次成功保存调用 + 1 次失败调用：错误率 1/10 = 10%（按调用次数量纲），
	// 超过默认 5%，但低于 50% 硬阈值 → degraded。
	for i := 0; i < 9; i++ {
		m.RecordEventSaved(1, time.Millisecond)
	}
	m.RecordEventStoreError()

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusDegraded {
		t.Fatalf("expected degraded, got %s (%s)", status, msg)
	}
	if !strings.Contains(msg, "event_store error_rate") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestMetricsHealthCheck_EventStoreUnhealthyHighRate(t *testing.T) {
	m := NewMetrics()
	// 1 次成功调用 + 15 次失败调用：错误率 93.75% ≥ 50% → unhealthy。
	m.RecordEventSaved(5, time.Millisecond)
	for i := 0; i < 15; i++ {
		m.RecordEventStoreError()
	}

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusUnhealthy {
		t.Fatalf("expected unhealthy, got %s (%s)", status, msg)
	}
}

// P2#27 回归：批量失败场景错误率不得被事件条数稀释。
// 一次成功批量调用落库 1,000,000 条 + 100 次失败调用：旧口径（分母=成功条数）
// 错误率 ≈ 0.01%，健康探针长期误报 healthy；新口径（分母=调用次数）错误率
// = 100/101 ≈ 99%，必须直接判 unhealthy。
func TestMetricsHealthCheck_EventStoreBatchFailureNotDiluted(t *testing.T) {
	m := NewMetrics()
	m.RecordEventSaved(1000000, time.Millisecond)
	for i := 0; i < 100; i++ {
		m.RecordEventStoreError()
	}

	if got := m.EventStoreErrorRatePercent(); math.Abs(got-100.0/101.0*100) > 1e-9 {
		t.Fatalf("expected call-based error rate ~99.0%%, got %.4f%%", got)
	}

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusUnhealthy {
		t.Fatalf("batch failure must not be diluted: expected unhealthy, got %s (%s)", status, msg)
	}
	if !strings.Contains(msg, "event_store error_rate") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

// 读写混合量纲：读失败也计入分子，成功加载调用计入分母，比率按调用次数自洽。
func TestMetricsHealthCheck_EventStoreMixedReadWriteCalls(t *testing.T) {
	m := NewMetrics()
	m.RecordEventSaved(100, time.Millisecond) // 1 次保存调用
	m.RecordEventLoaded(50, time.Millisecond) // 1 次加载调用
	m.RecordEventStoreError()                 // 1 次失败调用

	// 1/3 ≈ 33.3%：degraded（>5%）且 unhealthy（<50%）。
	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusDegraded {
		t.Fatalf("expected degraded, got %s (%s)", status, msg)
	}
	if !strings.Contains(msg, "33.33%") {
		t.Fatalf("expected call-based rate 33.33%% in message, got: %s", msg)
	}
}

func TestMetricsHealthCheck_ProjectionLagDegraded(t *testing.T) {
	m := NewMetrics()
	m.RecordProjectionUpdate(true, 10*time.Second)

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusDegraded {
		t.Fatalf("expected degraded, got %s (%s)", status, msg)
	}
	if !strings.Contains(msg, "projection lag") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

// P2#9 回归：分母不得把失败数重复计入。
// 10 次尝试、6 次失败：真实错误率 60%。旧口径分母 updates+errors=16 → 37.5%，
// 会被低估为 degraded 并漏报 unhealthy；新口径 60% ≥ 50% → unhealthy。
func TestMetricsHealthCheck_ProjectionHighErrorRateUnhealthyNotDiluted(t *testing.T) {
	m := NewMetrics()
	for i := 0; i < 10; i++ {
		m.RecordProjectionUpdate(i < 4, time.Millisecond)
	}

	if s := m.Snapshot(); s.ProjectionUpdates != 10 || s.ProjectionErrors != 6 {
		t.Fatalf("unexpected counters: updates=%d errors=%d", s.ProjectionUpdates, s.ProjectionErrors)
	}

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusUnhealthy {
		t.Fatalf("60%% projection error rate must be unhealthy, got %s (%s)", status, msg)
	}
	if !strings.Contains(msg, "60.00%") {
		t.Fatalf("expected 60.00%% error rate in message, got: %s", msg)
	}
}

// 全失败场景：错误率必须为 100% 且判 unhealthy。
func TestMetricsHealthCheck_ProjectionAllUpdatesFailed(t *testing.T) {
	m := NewMetrics()
	// RecordProjectionUpdate 每次都会递增 updates，故 errors == updates 即全失败。
	for i := 0; i < 3; i++ {
		m.RecordProjectionUpdate(false, time.Millisecond)
	}

	check, err := MetricsHealthCheck(m, DefaultMetricsHealthConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusUnhealthy {
		t.Fatalf("expected unhealthy, got %s (%s)", status, msg)
	}
	if !strings.Contains(msg, "projection all updates failed") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

// summary 与 health 的错误率公式必须一致（P2#27）。
func TestMetricsErrorRateSummaryHealthAligned(t *testing.T) {
	m := NewMetrics()
	m.RecordEventSaved(500, time.Millisecond) // 1 次保存调用
	m.RecordEventSaved(500, time.Millisecond) // 1 次保存调用
	m.RecordEventLoaded(10, time.Millisecond) // 1 次加载调用
	m.RecordEventStoreError()                 // 1 次失败调用
	// 事件存储错误率 = 1/(2+1+1) = 25%。
	m.RecordProjectionUpdate(true, time.Millisecond)
	m.RecordProjectionUpdate(true, time.Millisecond)
	m.RecordProjectionUpdate(false, time.Millisecond)
	m.RecordProjectionUpdate(false, time.Millisecond)
	// 投影错误率 = 2/4 = 50%。

	s := m.Snapshot()
	summary := s.Summary()
	if math.Abs(summary.EventStore.ErrorRatePercent-25.0) > 1e-9 {
		t.Fatalf("summary event_store error rate = %.4f%%, want 25%%", summary.EventStore.ErrorRatePercent)
	}
	if math.Abs(summary.Projection.ErrorRatePercent-50.0) > 1e-9 {
		t.Fatalf("summary projection error rate = %.4f%%, want 50%%", summary.Projection.ErrorRatePercent)
	}

	check, err := MetricsHealthCheck(m, MetricsHealthConfig{
		MaxEventStoreErrorRatePercent: 5,
		MaxProjectionErrorRatePercent: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, msg, _ := check(context.Background())
	if !strings.Contains(msg, "event_store error_rate 25.00%") {
		t.Fatalf("health event_store rate must match summary (25%%), got: %s", msg)
	}
	// 投影 50% ≥ 50% 硬阈值 → unhealthy，且两处口径一致。
	if !strings.Contains(msg, "projection error_rate 50.00%") && !strings.Contains(msg, "projection all updates failed") {
		t.Fatalf("health projection rate must match summary (50%%), got: %s", msg)
	}
}

func TestMetricsHealthCheck_ZeroThresholdsSkipChecks(t *testing.T) {
	m := NewMetrics()
	m.RecordEventStoreError()
	m.RecordProjectionUpdate(false, 30*time.Second)

	// 阈值为 0：关闭对应检查，全部失败仍视为 healthy。
	check, err := MetricsHealthCheck(m, MetricsHealthConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, msg, _ := check(context.Background())
	if status != HealthStatusHealthy {
		t.Fatalf("expected healthy, got %s (%s)", status, msg)
	}
}
