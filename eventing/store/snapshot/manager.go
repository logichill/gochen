package snapshot

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"gochen/eventing/monitoring"
)

// Manager 负责判断何时创建快照、保存快照以及从快照恢复聚合。
type Manager[ID comparable] struct {
	snapshotStore ISnapshotStore[ID]
	config        *Config
	strategy      ISnapshotStrategy[ID] // 快照策略
	metrics       atomic.Value          // snapshotMetricsHolder（承载 monitoring.ISnapshotMetricsRecorder），用于并发热替换且避免 data race
	mutex         sync.RWMutex
}

type snapshotMetricsHolder struct {
	rec monitoring.ISnapshotMetricsRecorder
}

// NewManager 创建一个基于给定存储与配置的快照管理器。
func NewManager[ID comparable](snapshotStore ISnapshotStore[ID], config *Config) *Manager[ID] {
	config = normalizeConfig(config)
	defaultStrategy := NewEventCountStrategy[ID](config.Frequency)
	manager := &Manager[ID]{snapshotStore: snapshotStore, config: config, strategy: defaultStrategy}
	injectSnapshotStore(defaultStrategy, snapshotStore)
	return manager
}

// SetMetricsRecorder 设置快照指标记录器（可选）。
//
// 并发语义：允许在运行时注入/替换 recorder（内部使用 atomic.Value），避免与 Record* 并发访问时产生 data race。
func (sm *Manager[ID]) SetMetricsRecorder(rec monitoring.ISnapshotMetricsRecorder) {
	if sm == nil {
		return
	}
	if isNilMetricsRecorder(rec) {
		rec = nil
	}
	// atomic.Value 不允许 Store(nil)，且要求后续 Store 的动态类型一致。
	// 用稳定的 holder 类型承载 interface，可以安全地设置 nil/不同实现。
	sm.metrics.Store(snapshotMetricsHolder{rec: rec})
}

func isNilMetricsRecorder(rec monitoring.ISnapshotMetricsRecorder) bool {
	if rec == nil {
		return true
	}
	rv := reflect.ValueOf(rec)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func (sm *Manager[ID]) getMetrics() monitoring.ISnapshotMetricsRecorder {
	if sm == nil {
		return nil
	}
	v := sm.metrics.Load()
	if v == nil {
		return nil
	}
	h, ok := v.(snapshotMetricsHolder)
	if !ok || isNilMetricsRecorder(h.rec) {
		return nil
	}
	return h.rec
}

// SetStrategy 替换当前使用的快照触发策略。
//
// 会自动把 Manager 持有的 SnapshotStore 注入到支持该能力的策略（含组合策略
// 的子策略），避免调用方忘记注入导致策略退化为绝对版本号判断。
func (sm *Manager[ID]) SetStrategy(strategy ISnapshotStrategy[ID]) {
	injectSnapshotStore(strategy, sm.snapshotStore)
	sm.mutex.Lock()
	sm.strategy = strategy
	sm.mutex.Unlock()
}

// snapshotStoreSetter 由需要快照存储才能做增量判断的策略实现。
type snapshotStoreSetter[ID comparable] interface {
	SetSnapshotStore(store ISnapshotStore[ID])
}

// injectSnapshotStore 递归向策略树注入快照存储（组合策略下钻到子策略）。
func injectSnapshotStore[ID comparable](strategy ISnapshotStrategy[ID], store ISnapshotStore[ID]) {
	if strategy == nil || store == nil {
		return
	}
	if setter, ok := strategy.(snapshotStoreSetter[ID]); ok {
		setter.SetSnapshotStore(store)
	}
	if composite, ok := strategy.(interface {
		Strategies() []ISnapshotStrategy[ID]
	}); ok {
		for _, child := range composite.Strategies() {
			injectSnapshotStore(child, store)
		}
	}
}

func (sm *Manager[ID]) Strategy() ISnapshotStrategy[ID] {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.strategy
}

// notifySnapshotCreated 把快照创建事实广播给实现了时间缓存更新的策略。
//
// 组合策略需要递归下钻，否则包在 CompositeSnapshotStrategy 里的
// TimeDurationStrategy 永远收不到通知。
func (sm *Manager[ID]) notifySnapshotCreated(aggregateType string, aggregateID ID, timestamp time.Time) {
	notifyStrategySnapshotCreated(sm.Strategy(), aggregateType, aggregateID, timestamp)
}

// snapshotTimeUpdater 由需要感知“快照已创建”的策略实现。
type snapshotTimeUpdater[ID comparable] interface {
	UpdateLastSnapshotTime(aggregateType string, aggregateID ID, timestamp time.Time)
}

func notifyStrategySnapshotCreated[ID comparable](strategy ISnapshotStrategy[ID], aggregateType string, aggregateID ID, timestamp time.Time) {
	if strategy == nil {
		return
	}
	if updater, ok := strategy.(snapshotTimeUpdater[ID]); ok {
		updater.UpdateLastSnapshotTime(aggregateType, aggregateID, timestamp)
	}
	if composite, ok := strategy.(interface {
		Strategies() []ISnapshotStrategy[ID]
	}); ok {
		for _, child := range composite.Strategies() {
			notifyStrategySnapshotCreated(child, aggregateType, aggregateID, timestamp)
		}
	}
}
