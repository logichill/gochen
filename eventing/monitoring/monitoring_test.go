package monitoring

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

// TestNewHTTPHandler_Endpoints 验证 NewHTTPHandler Endpoints。
func TestNewHTTPHandler_Endpoints(t *testing.T) {
	reg, err := NewRegistry()
	require.NoError(t, err)
	require.NoError(t, reg.Health.Register("always_unhealthy", func(context.Context) (HealthStatus, string, error) {
		return HealthStatusUnhealthy, "boom", nil
	}))

	h := NewHTTPHandler(reg)

	t.Run("healthz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)

		var rep HealthReport
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&rep))
		require.Equal(t, HealthStatusUnhealthy, rep.Status)
	})

	t.Run("readyz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)

		var rep HealthReport
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&rep))
		require.Equal(t, HealthStatusUnhealthy, rep.Status)
	})

	t.Run("metrics", func(t *testing.T) {
		reg.Metrics.RecordEventSaved(2, 3*time.Millisecond)
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var summary Summary
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&summary))
		require.GreaterOrEqual(t, summary.EventStore.EventsSaved, int64(2))
	})

	t.Run("snapshot", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/snapshot", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
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
