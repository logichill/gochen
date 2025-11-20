package monitoring

import "sync"

var globalMonitoring *MonitoringService
var monitoringOnce sync.Once

// InitGlobalMonitoring 初始化全局监控服务
func InitGlobalMonitoring(eventStore any, snapshotMgr any, cachedStore CachedStore) {
	monitoringOnce.Do(func() { globalMonitoring = NewMonitoringService(eventStore, snapshotMgr, cachedStore) })
}

// GlobalMetrics 获取全局指标收集器
func GlobalMetrics() *Metrics {
	if globalMonitoring == nil {
		return NewMetrics()
	}
	return globalMonitoring.metrics
}

// GlobalMonitoring 获取全局监控服务
func GlobalMonitoring() *MonitoringService { return globalMonitoring }
