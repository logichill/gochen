package monitoring

import (
	"context"
	"sync"
	"time"
)

// MonitoringService 监控服务
type MonitoringService struct {
	metrics     *Metrics
	eventStore  any         // 事件存储接口
	snapshotMgr any         // 快照管理器接口
	cachedStore ICachedStore // 可选
	mutex       sync.RWMutex
}

// NewMonitoringService 创建监控服务
func NewMonitoringService(eventStore any, snapshotMgr any, cachedStore ICachedStore) *MonitoringService {
	return &MonitoringService{
		metrics:     NewMetrics(),
		eventStore:  eventStore,
		snapshotMgr: snapshotMgr,
		cachedStore: cachedStore,
	}
}

// GetMetrics 获取全局指标
func (s *MonitoringService) GetMetrics() *Metrics { return s.metrics }

// GetHealthStatus 获取健康状态
func (s *MonitoringService) GetHealthStatus(ctx context.Context) map[string]any {
	snapshot := s.metrics.GetSnapshot()

	healthy := true
	issues := make([]string, 0)

	if snapshot.EventStoreErrors > 0 {
		errRate := float64(snapshot.EventStoreErrors) / float64(snapshot.EventsSaved) * 100
		if errRate > 5.0 {
			healthy = false
			issues = append(issues, "High event store error rate")
		}
	}

	if snapshot.ProjectionLag > 5*time.Second {
		healthy = false
		issues = append(issues, "High projection lag")
	}

	if snapshot.ProjectionErrors > 0 {
		errRate := float64(snapshot.ProjectionErrors) / float64(snapshot.ProjectionUpdates) * 100
		if errRate > 5.0 {
			healthy = false
			issues = append(issues, "High projection error rate")
		}
	}

	status := "healthy"
	if !healthy {
		status = "degraded"
	}

	return map[string]any{
		"status":  status,
		"healthy": healthy,
		"issues":  issues,
		"uptime":  snapshot.Uptime.Seconds(),
		"metrics": snapshot.ToMap(),
	}
}

// GetDetailedStats 获取详细统计信息
func (s *MonitoringService) GetDetailedStats(ctx context.Context) map[string]any {
	result := make(map[string]any)
	result["metrics"] = s.metrics.GetSnapshot().ToMap()

	if s.snapshotMgr != nil {
		if snapshotter, ok := s.snapshotMgr.(interface {
			GetSnapshotStats(context.Context) map[string]any
		}); ok {
			snapshotStats := snapshotter.GetSnapshotStats(ctx)
			result["snapshot_manager"] = snapshotStats
		}
	}

	if s.cachedStore != nil {
		hits, misses := s.cachedStore.GetCacheStats()
		result["cache"] = map[string]int64{"hits": hits, "misses": misses}
	}

	return result
}
