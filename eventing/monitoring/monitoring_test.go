package monitoring

import (
	"context"
	"errors"
	"testing"
	"time"

	"gochen/testkit/require"
)

// TestDefaultRegistry_MetricsIsStableInstance 验证 DefaultRegistry MetricsIsStableInstance。
func TestDefaultRegistry_MetricsIsStableInstance(t *testing.T) {
	m1 := DefaultRegistry().Metrics
	m2 := DefaultRegistry().Metrics
	require.Same(t, m1, m2)

	m1.RecordEventSaved(1, 10*time.Millisecond)
	s := m2.Snapshot()
	require.GreaterOrEqual(t, s.EventsSaved, int64(1))
}

func TestMetricsProjectionLagKeepsDurationPrecision(t *testing.T) {
	metrics := NewMetrics()

	metrics.RecordProjectionUpdate(false, 1500*time.Millisecond)

	snapshot := metrics.Snapshot()
	require.Equal(t, int64(1), snapshot.ProjectionUpdates)
	require.Equal(t, int64(1), snapshot.ProjectionErrors)
	require.Equal(t, 1500*time.Millisecond, snapshot.ProjectionLag)
	require.Equal(t, int64(1500), snapshot.Summary().Projection.LagMillis)
}

// TestHealthRegistry_ReportAggregatesWorstStatus 验证 HealthRegistry ReportAggregatesWorstStatus。
func TestHealthRegistry_ReportAggregatesWorstStatus(t *testing.T) {
	hr := NewHealthRegistry()
	require.NoError(t, hr.Register("ok", func(context.Context) (HealthStatus, string, error) { return HealthStatusHealthy, "ok", nil }))
	require.NoError(t, hr.Register("warn", func(context.Context) (HealthStatus, string, error) { return HealthStatusDegraded, "warn", nil }))
	require.NoError(t, hr.Register("bad", func(context.Context) (HealthStatus, string, error) { return HealthStatusUnhealthy, "bad", nil }))

	rep := hr.Report(context.Background())
	require.Equal(t, HealthStatusUnhealthy, rep.Status)
	require.Len(t, rep.Checks, 3)
	require.Equal(t, "ok", rep.Checks[0].Name)
	require.Equal(t, "warn", rep.Checks[1].Name)
	require.Equal(t, "bad", rep.Checks[2].Name)
}

func TestHandleRouteSanitizesHealthDetails(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)
	require.NoError(t, reg.Health.Register("always_unhealthy", func(context.Context) (HealthStatus, string, error) {
		return HealthStatusUnhealthy, "postgres://user:secret@example/db", nil
	}))

	code, body := HandleRoute(context.Background(), reg, Routes(DefaultRouteSet())[0])
	require.Equal(t, statusServiceUnavailable, code)
	report := body.(HealthReport)
	require.NotEmpty(t, report.Checks)
	for _, check := range report.Checks {
		require.Empty(t, check.Message)
		require.Empty(t, check.Error)
	}
}

type failingSnapshotStatsProvider struct{}

func (failingSnapshotStatsProvider) SnapshotStats(context.Context) (SnapshotManagerStats, error) {
	return SnapshotManagerStats{}, errors.New("postgres://user:secret@example/snapshots")
}

type failingOutboxMetricsProvider struct{}

func (failingOutboxMetricsProvider) Snapshot(context.Context) (*OutboxSnapshot, error) {
	return nil, errors.New("postgres://user:secret@example/outbox")
}

func (failingOutboxMetricsProvider) Health(context.Context) (HealthStatus, string, error) {
	return HealthStatusUnhealthy, "outbox secret", errors.New("outbox health secret")
}

func TestHandleRouteSanitizesSnapshotProviderErrors(t *testing.T) {
	reg, err := NewRegistry(
		WithSnapshotStatsProvider(failingSnapshotStatsProvider{}),
		WithOutboxMetricsProvider(failingOutboxMetricsProvider{}),
	)
	require.NoError(t, err)

	var snapshotRoute Route
	for _, route := range Routes(FullRouteSet()) {
		if route.Name == RouteSnapshot {
			snapshotRoute = route
			break
		}
	}
	require.NotNil(t, snapshotRoute.Handle)

	_, body := HandleRoute(context.Background(), reg, snapshotRoute)
	snapshot := body.(Snapshot)
	require.NotNil(t, snapshot.SnapshotManager)
	require.Empty(t, snapshot.SnapshotManager.Error)
	require.NotNil(t, snapshot.Outbox)
	require.Empty(t, snapshot.Outbox.Error)
	for _, check := range snapshot.Health.Checks {
		require.Empty(t, check.Message)
		require.Empty(t, check.Error)
	}
}

func TestNewRegistry_MetricsWiring_CheckIsInfoByDefault(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)

	rep := reg.Health.Report(context.Background())
	var found bool
	for _, c := range rep.Checks {
		if c.Name != "eventing.metrics_wiring" {
			continue
		}
		found = true
		// 默认语义：all-zero 仅提示信息，不影响 overall health。
		require.Equal(t, HealthStatusHealthy, c.Status)
		break
	}
	require.True(t, found)
}
