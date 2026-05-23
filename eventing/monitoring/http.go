package monitoring

import (
	"encoding/json"
	"net/http"
)

// NewHTTPHandler 暴露 `/healthz`、`/readyz`、`/metrics` 和 `/snapshot` 监控端点。
func NewHTTPHandler(reg *Registry) http.Handler {
	if reg == nil {
		reg = DefaultRegistry()
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		report := reg.Health.Report(r.Context())
		code := http.StatusOK
		if report.Status == HealthStatusUnhealthy {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, report)
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		report := reg.Health.Report(r.Context())
		code := http.StatusOK
		if report.Status == HealthStatusUnhealthy {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, report)
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		summary := reg.Metrics.Snapshot().Summary()
		writeJSON(w, http.StatusOK, summary)
	})

	mux.HandleFunc("/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snapshot := reg.Snapshot(r.Context())
		code := http.StatusOK
		if snapshot.Health.Status == HealthStatusUnhealthy {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, snapshot)
	})

	return mux
}

// writeJSON 以 JSON 格式写回监控接口响应。
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
