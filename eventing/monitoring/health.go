package monitoring

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gochen/errors"
)

// HealthStatus 表示健康状态。
type HealthStatus string

const (
	// HealthStatusHealthy 表示健康。
	HealthStatusHealthy HealthStatus = "healthy"
	// HealthStatusDegraded 表示降级（可用但有风险/需要关注）。
	HealthStatusDegraded HealthStatus = "degraded"
	// HealthStatusUnhealthy 表示不健康（应触发告警或摘除流量）。
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// Check 是单项健康检查。
//
// 返回值约定：
// - status：检查输出的健康状态；
// - message：人类可读的简短描述；
// - err：检查执行失败的错误（非业务故障也应返回 err，例如探测超时）。
type Check func(ctx context.Context) (status HealthStatus, message string, err error)

// CheckResult 是单项健康检查结果。
type CheckResult struct {
	Name           string       `json:"name"`
	Status         HealthStatus `json:"status"`
	Message        string       `json:"message,omitempty"`
	Error          string       `json:"error,omitempty"`
	DurationMillis int64        `json:"duration_ms"`
}

// HealthReport 是整体健康报告。
type HealthReport struct {
	Timestamp time.Time     `json:"timestamp"`
	Status    HealthStatus  `json:"status"`
	Checks    []CheckResult `json:"checks"`
}

// HealthRegistry 管理一组健康检查。
type HealthRegistry struct {
	mu     sync.RWMutex
	order  []string
	checks map[string]Check
}

// NewHealthRegistry 创建健康检查注册表。
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{
		checks: make(map[string]Check),
	}
}

// Register 注册或覆盖一个检查项，并保持输出顺序与首次注册顺序一致。
func (r *HealthRegistry) Register(name string, check Check) error {
	if name == "" {
		return errors.NewCode(errors.InvalidInput, "health check name cannot be empty")
	}
	if check == nil {
		return errors.NewCode(errors.InvalidInput, "health check cannot be nil").WithContext("name", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.checks[name]; !exists {
		r.order = append(r.order, name)
	}
	r.checks[name] = check
	return nil
}

// Unregister 移除一个检查项，并返回是否真的删除了已注册项。
func (r *HealthRegistry) Unregister(name string) bool {
	if name == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.checks[name]; !ok {
		return false
	}
	delete(r.checks, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return true
}

// Report 按注册顺序执行全部检查，并生成一份可直接对外输出的健康报告。
func (r *HealthRegistry) Report(ctx context.Context) HealthReport {
	if r == nil {
		return HealthReport{Timestamp: Now(), Status: HealthStatusUnhealthy}
	}

	r.mu.RLock()
	names := append([]string(nil), r.order...)
	checks := make([]Check, 0, len(names))
	for _, name := range names {
		checks = append(checks, r.checks[name])
	}
	r.mu.RUnlock()

	results := make([]CheckResult, 0, len(names))
	overall := HealthStatusHealthy

	for i, name := range names {
		start := time.Now()
		status, msg, err := checks[i](ctx)
		d := time.Since(start)

		res := CheckResult{
			Name:           name,
			Status:         status,
			Message:        msg,
			DurationMillis: d.Milliseconds(),
		}
		if err != nil {
			res.Error = err.Error()
			if res.Status == "" {
				res.Status = HealthStatusUnhealthy
			}
		}
		if res.Status == "" {
			res.Status = HealthStatusHealthy
		}

		overall = worseStatus(overall, res.Status)
		results = append(results, res)
	}

	return HealthReport{
		Timestamp: Now(),
		Status:    overall,
		Checks:    results,
	}
}

func worseStatus(a, b HealthStatus) HealthStatus {
	// 顺序：healthy < degraded < unhealthy
	rank := func(s HealthStatus) int {
		switch s {
		case HealthStatusHealthy:
			return 0
		case HealthStatusDegraded:
			return 1
		case HealthStatusUnhealthy:
			return 2
		default:
			return 2
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// MetricsHealthConfig 定义指标健康检查配置。
type MetricsHealthConfig struct {
	MaxEventStoreErrorRatePercent float64
	MaxProjectionLag              time.Duration
	MaxProjectionErrorRatePercent float64
}

func DefaultMetricsHealthConfig() MetricsHealthConfig {
	return MetricsHealthConfig{
		MaxEventStoreErrorRatePercent: 5,
		MaxProjectionLag:              5 * time.Second,
		MaxProjectionErrorRatePercent: 5,
	}
}

// MetricsHealthCheck 基于内存指标快照生成一个通用健康检查函数。
func MetricsHealthCheck(metrics *Metrics, cfg MetricsHealthConfig) (Check, error) {
	if metrics == nil {
		return nil, errors.NewCode(errors.InvalidInput, "metrics cannot be nil")
	}

	return func(context.Context) (HealthStatus, string, error) {
		s := metrics.Snapshot()

		var issues []string

		if s.EventsSaved > 0 && cfg.MaxEventStoreErrorRatePercent > 0 {
			rate := errorRatePercent(s.EventStoreErrors, s.EventsSaved)
			if rate > cfg.MaxEventStoreErrorRatePercent {
				issues = append(issues, fmt.Sprintf("event_store error_rate %.2f%% > %.2f%%", rate, cfg.MaxEventStoreErrorRatePercent))
			}
		}

		if cfg.MaxProjectionLag > 0 && s.ProjectionLag > cfg.MaxProjectionLag {
			issues = append(issues, fmt.Sprintf("projection lag %s > %s", s.ProjectionLag, cfg.MaxProjectionLag))
		}

		if s.ProjectionUpdates > 0 && cfg.MaxProjectionErrorRatePercent > 0 {
			rate := errorRatePercent(s.ProjectionErrors, s.ProjectionUpdates)
			if rate > cfg.MaxProjectionErrorRatePercent {
				issues = append(issues, fmt.Sprintf("projection error_rate %.2f%% > %.2f%%", rate, cfg.MaxProjectionErrorRatePercent))
			}
		}

		if len(issues) == 0 {
			return HealthStatusHealthy, "ok", nil
		}
		return HealthStatusDegraded, strings.Join(issues, "; "), nil
	}, nil
}
