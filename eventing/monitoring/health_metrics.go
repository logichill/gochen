package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gochen/errors"
)

// MetricsHealthConfig 定义指标健康检查配置。
type MetricsHealthConfig struct {
	MaxEventStoreErrorRatePercent float64
	MaxProjectionLag              time.Duration
	MaxProjectionErrorRatePercent float64
}

// unhealthyErrorRatePercent 错误率超过该阈值（%）时直接判为 unhealthy，
// 而不仅是 degraded；近全量故障必须触发告警/摘流而非仅降险提醒。
const unhealthyErrorRatePercent = 50.0

func DefaultMetricsHealthConfig() MetricsHealthConfig {
	return MetricsHealthConfig{
		MaxEventStoreErrorRatePercent: 5,
		MaxProjectionLag:              5 * time.Second,
		MaxProjectionErrorRatePercent: 5,
	}
}

// 错误率口径（与 MetricsSnapshot.Summary().EventStore/Projection 完全一致）：
//
//   - 事件存储错误率 = EventStoreErrors / (EventStoreSaves + EventStoreLoads
//
//   - EventStoreErrors)。分子与分母均为「调用次数」：RecordEventStoreError
//     每次失败调用 +1（读写路径均计入），RecordEventSaved/RecordEventLoaded
//     每次成功调用各 +1（无论单条还是批量）。此前分母使用「成功事件条数」
//     （EventsSaved），与分子的调用次数量纲不齐——批量保存下一次失败调用丢
//     N 条事件只记 1 次失败，错误率被稀释约 N 倍，≥50% unhealthy 阈值在有
//     成功历史的批量场景下几乎不可达。
//
//   - 投影错误率 = ProjectionErrors / ProjectionUpdates。
//     RecordProjectionUpdate 每次尝试（无论成败）都会自增 ProjectionUpdates，
//     因此分母就是总尝试次数；不得把 ProjectionErrors 重复加进分母
//     （那会把失败计两次，错误率被系统性低估，≥50% unhealthy 升级漏报）。
func MetricsHealthCheck(metrics *Metrics, cfg MetricsHealthConfig) (Check, error) {
	if metrics == nil {
		return nil, errors.NewCode(errors.InvalidInput, "metrics cannot be nil")
	}

	return func(context.Context) (HealthStatus, string, error) {
		s := metrics.Snapshot()

		var issues []string
		unhealthy := false

		// 事件存储：全部调用失败（无任何成功调用但有失败）必须直接判 unhealthy，
		// 否则全量故障场景下健康探针仍误报 healthy，导致 K8s 无法摘流。
		eventStoreCalls := s.EventStoreSaves + s.EventStoreLoads + s.EventStoreErrors
		if cfg.MaxEventStoreErrorRatePercent > 0 {
			switch {
			case eventStoreCalls == s.EventStoreErrors && s.EventStoreErrors > 0:
				issues = append(issues, fmt.Sprintf("event_store all calls failed (errors=%d, success_calls=0)", s.EventStoreErrors))
				unhealthy = true
			case eventStoreCalls > 0:
				rate := errorRatePercent(s.EventStoreErrors, eventStoreCalls)
				if rate > cfg.MaxEventStoreErrorRatePercent {
					issues = append(issues, fmt.Sprintf("event_store error_rate %.2f%% > %.2f%%", rate, cfg.MaxEventStoreErrorRatePercent))
					if rate >= unhealthyErrorRatePercent {
						unhealthy = true
					}
				}
			}
		}

		if cfg.MaxProjectionLag > 0 && s.ProjectionLag > cfg.MaxProjectionLag {
			issues = append(issues, fmt.Sprintf("projection lag %s > %s", s.ProjectionLag, cfg.MaxProjectionLag))
		}

		// 投影：ProjectionUpdates 本身就是总尝试次数（成败都 +1），
		// 分母不得重复计入失败数。
		if cfg.MaxProjectionErrorRatePercent > 0 && s.ProjectionUpdates > 0 {
			switch {
			case s.ProjectionErrors >= s.ProjectionUpdates:
				// 全部尝试失败：错误率 100%，直接 unhealthy（该分支同时兜底
				// errors 因并发记账短暂超过 updates 的极端读数）。
				issues = append(issues, fmt.Sprintf("projection all updates failed (errors=%d, updates=%d)", s.ProjectionErrors, s.ProjectionUpdates))
				unhealthy = true
			default:
				rate := errorRatePercent(s.ProjectionErrors, s.ProjectionUpdates)
				if rate > cfg.MaxProjectionErrorRatePercent {
					issues = append(issues, fmt.Sprintf("projection error_rate %.2f%% > %.2f%%", rate, cfg.MaxProjectionErrorRatePercent))
					if rate >= unhealthyErrorRatePercent {
						unhealthy = true
					}
				}
			}
		}

		if len(issues) == 0 {
			return HealthStatusHealthy, "ok", nil
		}
		if unhealthy {
			return HealthStatusUnhealthy, strings.Join(issues, "; "), nil
		}
		return HealthStatusDegraded, strings.Join(issues, "; "), nil
	}, nil
}
