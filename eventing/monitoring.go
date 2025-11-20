package eventing

import (
	mon "gochen/eventing/monitoring"
)

// 类型别名与向后兼容包装（保持旧引用不破坏）
type (
	MonitoringService = mon.MonitoringService
	Metrics           = mon.Metrics
	MetricsSnapshot   = mon.MetricsSnapshot
	CachedStore       = mon.CachedStore
)

func NewMonitoringService(eventStore any, snapshotMgr any, cachedStore CachedStore) *MonitoringService {
	return mon.NewMonitoringService(eventStore, snapshotMgr, cachedStore)
}
func InitGlobalMonitoring(eventStore any, snapshotMgr any, cachedStore CachedStore) {
	mon.InitGlobalMonitoring(eventStore, snapshotMgr, cachedStore)
}
func GlobalMetrics() *Metrics              { return mon.GlobalMetrics() }
func GlobalMonitoring() *MonitoringService { return mon.GlobalMonitoring() }
