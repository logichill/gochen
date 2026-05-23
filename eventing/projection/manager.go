package projection

import (
	"sync"
	"sync/atomic"
	"time"

	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/monitoring"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/logging"
)

// ProjectionManager 表示投影管理器。
type ProjectionManager[ID comparable] struct {
	runtimes        map[string]*projectionRuntime[ID]
	eventStore      store.IEventStreamStore[ID]
	eventBus        bus.IEventBus
	config          *ProjectionConfig
	checkpointStore ICheckpointStore // 检查点存储（可选）
	mutex           sync.RWMutex
	logger          logging.ILogger

	eventRegistry *registry.Registry
	upgraders     *upcast.UpgraderRegistry
	metrics       atomic.Value // projectionMetricsHolder（承载 monitoring.IProjectionMetricsRecorder），用于并发热替换且避免 data race
}

type projectionMetricsHolder struct {
	rec monitoring.IProjectionMetricsRecorder
}

// checkpointState 检查点批量保存状态。
type checkpointState struct {
	lastSaveTime        time.Time // 上次保存检查点时间
	eventsSinceLastSave int       // 自上次保存后处理的事件数
}

const replayBatchLimit = 1000 // 单次回放批量上限，避免一次性加载过多事件

// NewProjectionManager 创建投影管理器。
func NewProjectionManager[ID comparable](
	eventStore store.IEventStreamStore[ID],
	eventBus bus.IEventBus,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (*ProjectionManager[ID], error) {
	return NewProjectionManagerWithConfig(eventStore, eventBus, reg, upgraders, nil)
}

// NewProjectionManagerWithConfig 创建投影管理器并带配置。
func NewProjectionManagerWithConfig[ID comparable](
	eventStore store.IEventStreamStore[ID],
	eventBus bus.IEventBus,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
	config *ProjectionConfig,
) (*ProjectionManager[ID], error) {
	config = normalizeProjectionConfig(config)
	if eventBus == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event bus cannot be nil")
	}
	if reg == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event registry cannot be nil")
	}
	if upgraders == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil")
	}

	pm := &ProjectionManager[ID]{
		runtimes:      make(map[string]*projectionRuntime[ID]),
		eventStore:    eventStore,
		eventBus:      eventBus,
		config:        config,
		eventRegistry: reg,
		upgraders:     upgraders,
	}
	pm.logger = logging.ComponentLogger("projection.manager")
	return pm, nil
}

// WithCheckpointStore 配置检查点存储。
//
// 说明：
// - 启用检查点后，投影会在处理事件后自动保存位置，
// - 进程重启后可以从上次位置继续处理。
func (pm *ProjectionManager[ID]) WithCheckpointStore(store ICheckpointStore) (*ProjectionManager[ID], error) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if store != nil && pm.eventStore == nil {
		return pm, errors.NewCode(errors.InvalidInput, "checkpoint store requires event stream store")
	}
	for _, rt := range pm.runtimes {
		if err := validateCheckpointingProjection(rt.projection, store); err != nil {
			return pm, err
		}
	}
	pm.checkpointStore = store
	return pm, nil
}

// SetMetricsRecorder 设置投影指标记录器（可选）。
//
// 并发语义：允许在运行时注入/替换 recorder（内部使用 atomic.Value），避免与 Record* 并发访问时产生 data race。
func (pm *ProjectionManager[ID]) SetMetricsRecorder(rec monitoring.IProjectionMetricsRecorder) {
	// atomic.Value 不允许 Store(nil)，且要求后续 Store 的动态类型一致。
	// 用稳定的 holder 类型承载 interface，可以安全地设置 nil/不同实现。
	pm.metrics.Store(projectionMetricsHolder{rec: rec})
}

func (pm *ProjectionManager[ID]) getMetrics() monitoring.IProjectionMetricsRecorder {
	if pm == nil {
		return nil
	}
	v := pm.metrics.Load()
	if v == nil {
		return nil
	}
	h, ok := v.(projectionMetricsHolder)
	if !ok {
		return nil
	}
	return h.rec
}

func (pm *ProjectionManager[ID]) hasCheckpointStore() bool {
	if pm == nil {
		return false
	}
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return pm.checkpointStore != nil
}

func (pm *ProjectionManager[ID]) runtime(name string) (*projectionRuntime[ID], bool) {
	if pm == nil {
		return nil, false
	}
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	rt, ok := pm.runtimes[name]
	return rt, ok
}
