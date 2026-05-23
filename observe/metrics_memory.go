package observe

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// InMemoryMetrics 把指标保存在内存中，适合测试和开发调试场景。
type InMemoryMetrics struct {
	counters   map[string]*int64
	gauges     map[string]*float64
	histograms map[string][]float64
	mu         sync.RWMutex
}

// NewInMemoryMetrics 创建一个空的内存指标收集器。
func NewInMemoryMetrics() *InMemoryMetrics {
	return &InMemoryMetrics{
		counters:   make(map[string]*int64),
		gauges:     make(map[string]*float64),
		histograms: make(map[string][]float64),
	}
}

// Counter 累加一个计数器指标。
func (m *InMemoryMetrics) Counter(name string, value int64, labels map[string]string) {
	key := m.metricKey(name, labels)
	m.mu.Lock()
	ptr, ok := m.counters[key]
	if !ok {
		var v int64
		ptr = &v
		m.counters[key] = ptr
	}
	m.mu.Unlock()
	atomic.AddInt64(ptr, value)
}

// Gauge 覆盖设置一个仪表盘指标值。
func (m *InMemoryMetrics) Gauge(name string, value float64, labels map[string]string) {
	key := m.metricKey(name, labels)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gauges[key] == nil {
		v := value
		m.gauges[key] = &v
	} else {
		*m.gauges[key] = value
	}
}

// Histogram 追加一条直方图观测值。
func (m *InMemoryMetrics) Histogram(name string, value float64, labels map[string]string) {
	key := m.metricKey(name, labels)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histograms[key] = append(m.histograms[key], value)
}

// Timer 创建一个会在 Stop 时自动上报耗时的计时器。
func (m *InMemoryMetrics) Timer(name string, labels map[string]string) ITimer {
	return &inMemoryTimer{
		metrics: m,
		name:    name,
		labels:  labels,
		start:   time.Now(),
	}
}

// CounterValue 返回指定计数器的当前值，主要用于测试断言。
func (m *InMemoryMetrics) CounterValue(name string, labels map[string]string) int64 {
	key := m.metricKey(name, labels)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.counters[key]; ok {
		return atomic.LoadInt64(v)
	}
	return 0
}

// GaugeValue 返回指定仪表盘指标的当前值，主要用于测试断言。
func (m *InMemoryMetrics) GaugeValue(name string, labels map[string]string) float64 {
	key := m.metricKey(name, labels)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.gauges[key]; ok {
		return *v
	}
	return 0
}

func (m *InMemoryMetrics) HistogramValues(name string, labels map[string]string) []float64 {
	key := m.metricKey(name, labels)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if v, ok := m.histograms[key]; ok {
		result := make([]float64, len(v))
		copy(result, v)
		return result
	}
	return nil
}

// Reset 清空全部内存指标，主要用于测试重置。
func (m *InMemoryMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = make(map[string]*int64)
	m.gauges = make(map[string]*float64)
	m.histograms = make(map[string][]float64)
}

// metricKey 把指标名和标签集合拼成一个稳定的内存索引键。
func (m *InMemoryMetrics) metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	// 对标签键排序以确保相同标签集合生成相同 key
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	// 简单插入排序（标签数量通常很少）
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	var sb strings.Builder
	sb.WriteString(name)
	for _, k := range keys {
		sb.WriteByte(';')
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(labels[k])
	}
	return sb.String()
}

type inMemoryTimer struct {
	metrics *InMemoryMetrics
	name    string
	labels  map[string]string
	start   time.Time
}

// Stop 结束计时并把耗时写入直方图指标。
func (t *inMemoryTimer) Stop() {
	duration := float64(time.Since(t.start).Milliseconds())
	t.metrics.Histogram(t.name, duration, t.labels)
}

// 编译期断言：确保 InMemoryMetrics 实现 IMetrics。
var _ IMetrics = (*InMemoryMetrics)(nil)
