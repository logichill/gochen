package projection

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/monitoring"
	"gochen/eventing/registry"
	"gochen/eventing/store"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
)

// ProjectionManager 表示投影管理器。
type ProjectionManager[ID comparable] struct {
	runtimes        map[string]*projectionRuntime[ID]
	registering     map[string]*projectionRuntime[ID]
	eventStore      store.IEventStreamReader[ID]
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
	eventStore store.IEventStreamReader[ID],
	eventBus bus.IEventBus,
	reg *registry.Registry,
	upgraders *upcast.UpgraderRegistry,
) (*ProjectionManager[ID], error) {
	return NewProjectionManagerWithConfig(eventStore, eventBus, reg, upgraders, nil)
}

// NewProjectionManagerWithConfig 创建投影管理器并带配置。
func NewProjectionManagerWithConfig[ID comparable](
	eventStore store.IEventStreamReader[ID],
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
		registering:   make(map[string]*projectionRuntime[ID]),
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

func (pm *ProjectionManager[ID]) hasEventStore() bool {
	if pm == nil {
		return false
	}
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return pm.eventStore != nil
}

// hasRealGap 判断在线事件与投影游标之间是否存在**该投影真正关心**的缺口。
//
// 全局位置跨事件类型递增：投影只订阅部分类型时，位置差几乎总不为 1，
// 直接用 pos > cursor.Position+1 判定会把「别的类型的事件」误当成空洞，
// 导致每次在线事件都触发一轮伪追赶（重放退化）。这里改为向事件存储查询
// 游标之后、当前事件位置之前是否存在**本投影支持类型**的未处理事件：
//   - 存在 → 真实空洞，需要追赶；
//   - 不存在 → 位置差由其它类型事件造成，无需追赶。
//
// 返回值 (realGap, confirmed)：
//   - realGap=true 且 confirmed=true：存储查询成功并确认存在未处理的
//     本类型事件（真实空洞）。调用方必须立即追赶，不得被节流窗口抑制——
//     抑制会让在线游标单调越进未处理区间，空洞事件被永久埋没。
//   - realGap=true 且 confirmed=false：探测本身失败或存储未暴露位置，
//     无法判定（保守视为存在缺口）。调用方对此类缺口保留节流窗口，
//     避免存储异常时每个在线事件都发起一轮注定失败的追赶。
//   - realGap=false（此时 confirmed 恒为 true）：确认无缺口。
func (pm *ProjectionManager[ID]) hasRealGap(ctx context.Context, rt *projectionRuntime[ID], cursor *Checkpoint, targetPosition int64) (realGap bool, confirmed bool) {
	if pm == nil || rt == nil || rt.projection == nil || cursor == nil || targetPosition <= 0 {
		return false, true
	}
	pm.mutex.RLock()
	eventStore := pm.eventStore
	pm.mutex.RUnlock()
	if eventStore == nil {
		return false, true
	}
	stream, err := eventStore.StreamEvents(ctx, &store.StreamOptions{
		After: cursor.LastEventID,
		Types: rt.projection.SupportedEventTypes(),
		Limit: 1,
	})
	if err != nil {
		// 探测失败（如存储瞬断）：无法判定，保守认为存在缺口但未确认。
		return true, false
	}
	if stream == nil || len(stream.Events) == 0 {
		return false, true
	}
	// 取游标之后第一条支持类型事件：若其位置早于当前在线事件，说明中间
	// 确实有未处理的本类型事件（真实空洞）。
	if pos := stream.Events[0].GetGlobalPosition(); pos > 0 {
		return pos < targetPosition, true
	}
	// 存储未暴露位置：无法判定，保守认为存在缺口但未确认。
	return true, false
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
