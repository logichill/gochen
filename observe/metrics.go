package observe

import "time"

// IMetrics 指标接口。
//
// 提供应用指标收集能力，支持计数器、仪表盘和直方图。
type IMetrics interface {
	// Counter 增加计数器
	//
	// 计数器只能递增，用于记录请求数、错误数等。
	Counter(name string, value int64, labels map[string]string)

	// Gauge 设置仪表盘值
	//
	// 仪表盘可以任意设置值，用于记录当前连接数、队列长度等。
	Gauge(name string, value float64, labels map[string]string)

	// Histogram 记录直方图值
	//
	// 直方图用于记录数值分布，如请求延迟、响应大小等。
	Histogram(name string, value float64, labels map[string]string)

	// Timer 创建计时器
	//
	// 便捷方法，自动记录操作耗时到直方图。
	Timer(name string, labels map[string]string) ITimer
}

// ITimer 计时器接口。
type ITimer interface {
	// Stop 停止计时并记录
	Stop()
}

// MetricLabels 指标标签类型别名。
type MetricLabels = map[string]string

// DefaultMetrics 默认指标实现（无操作）
type DefaultMetrics struct{}

// Counter 记录一次计数器增量（默认实现为无操作）。
func (m *DefaultMetrics) Counter(name string, value int64, labels map[string]string) {
	_ = name
	_ = value
	_ = labels
}

// Gauge 记录一次仪表盘值（默认实现为无操作）。
func (m *DefaultMetrics) Gauge(name string, value float64, labels map[string]string) {
	_ = name
	_ = value
	_ = labels
}

// Histogram 记录一次直方图值（默认实现为无操作）。
func (m *DefaultMetrics) Histogram(name string, value float64, labels map[string]string) {
	_ = name
	_ = value
	_ = labels
}

func (m *DefaultMetrics) Timer(name string, labels map[string]string) ITimer {
	return &defaultTimer{metrics: m, name: name, labels: labels, start: time.Now()}
}

type defaultTimer struct {
	metrics *DefaultMetrics
	name    string
	labels  map[string]string
	start   time.Time
}

// Stop 停止计时并记录耗时。
func (t *defaultTimer) Stop() {
	t.metrics.Histogram(t.name, float64(time.Since(t.start).Milliseconds()), t.labels)
}

// 接口断言。
var _ IMetrics = (*DefaultMetrics)(nil)
