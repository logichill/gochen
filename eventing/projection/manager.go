package projection

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gochen/eventing"
	"gochen/eventing/bus"
	"gochen/eventing/store"
	"gochen/logging"
	"gochen/messaging"
)

var projectionLogger = logging.GetLogger().WithFields(
	logging.String("component", "projection.manager"),
)

// IProjection 投影接口
type IProjection interface {
	// 获取投影名称
	GetName() string

	// 处理事件
	Handle(ctx context.Context, event eventing.IEvent) error

	// 获取支持的事件类型
	GetSupportedEventTypes() []string

	// 重建投影
	Rebuild(ctx context.Context, events []eventing.Event) error

	// 获取投影状态
	GetStatus() ProjectionStatus
}

// ProjectionStatus 投影状态
type ProjectionStatus struct {
	Name            string    `json:"name"`
	LastEventID     string    `json:"last_event_id"`
	LastEventTime   time.Time `json:"last_event_time"`
	ProcessedEvents int64     `json:"processed_events"`
	FailedEvents    int64     `json:"failed_events"`
	Status          string    `json:"status"` // running, stopped, error
	LastError       string    `json:"last_error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ProjectionConfig 投影配置
//
// 用于配置投影的错误处理和重试策略。
type ProjectionConfig struct {
	// MaxRetries 最大重试次数（0 表示不重试）
	MaxRetries int

	// RetryBackoff 重试退避时间
	RetryBackoff time.Duration

	// DeadLetterFunc 死信处理函数（重试失败后调用）
	// 可用于记录日志、发送告警或将事件发送到死信队列
	DeadLetterFunc func(err error, event eventing.Event, projection string)
}

// DefaultProjectionConfig 默认投影配置
func DefaultProjectionConfig() *ProjectionConfig {
	return &ProjectionConfig{
		MaxRetries:   3,
		RetryBackoff: 1 * time.Second,
		DeadLetterFunc: func(err error, event eventing.Event, projection string) {
			projectionLogger.Error(context.Background(), "事件处理失败（已达最大重试次数）", logging.Error(err),
				logging.String("projection", projection),
				logging.String("event_id", event.ID),
				logging.String("event_type", event.Type),
			)
		},
	}
}

// ProjectionManager 投影管理器
//
// 注意：历史上该类型曾命名为 IProjectionManager（违反接口命名约定），
// 为保持向后兼容，仍保留 IProjectionManager 作为别名类型，但推荐新代码
// 直接使用 ProjectionManager。
type ProjectionManager struct {
	projections     map[string]IProjection
	eventStore      store.IEventStore
	eventBus        bus.IEventBus
	statuses        map[string]*ProjectionStatus
	handlers        map[string]map[string]*projectionEventHandler
	config          *ProjectionConfig
	checkpointStore ICheckpointStore // 检查点存储（可选）
	mutex           sync.RWMutex
}

// Deprecated: IProjectionManager 仅为向后兼容而保留，请使用 ProjectionManager。
type IProjectionManager = ProjectionManager

// NewProjectionManager 创建投影管理器
func NewProjectionManager(eventStore store.IEventStore, eventBus bus.IEventBus) *ProjectionManager {
	return NewProjectionManagerWithConfig(eventStore, eventBus, nil)
}

// NewProjectionManagerWithConfig 创建带配置的投影管理器
func NewProjectionManagerWithConfig(eventStore store.IEventStore, eventBus bus.IEventBus, config *ProjectionConfig) *ProjectionManager {
	if config == nil {
		config = DefaultProjectionConfig()
	}

	return &ProjectionManager{
		projections:     make(map[string]IProjection),
		eventStore:      eventStore,
		eventBus:        eventBus,
		statuses:        make(map[string]*ProjectionStatus),
		handlers:        make(map[string]map[string]*projectionEventHandler),
		config:          config,
		checkpointStore: nil, // 默认不启用检查点
	}
}

// WithCheckpointStore 配置检查点存储
//
// 启用检查点后，投影会在处理事件后自动保存位置，
// 进程重启后可以从上次位置继续处理。
//
// 参数：
//   - store: 检查点存储实例
//
// 返回：
//   - *ProjectionManager: 管理器实例（支持链式调用）
func (pm *ProjectionManager) WithCheckpointStore(store ICheckpointStore) *ProjectionManager {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.checkpointStore = store
	return pm
}

// ResumeFromCheckpoint 从检查点恢复投影
//
// 加载检查点并从上次位置继续处理事件。
// 如果检查点不存在，则从头开始。
//
// 参数：
//   - ctx: 上下文
//   - projectionName: 投影名称
//
// 返回：
//   - error: 恢复失败错误
//
// 注意：
//   - 需要先配置 checkpointStore
//   - 会自动启动投影
func (pm *ProjectionManager) ResumeFromCheckpoint(ctx context.Context, projectionName string) error {
	pm.mutex.RLock()
	checkpointStore := pm.checkpointStore
	eventStore := pm.eventStore
	projection, exists := pm.projections[projectionName]
	pm.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("projection %s not found", projectionName)
	}

	if checkpointStore == nil {
		projectionLogger.Warn(ctx, "检查点存储未配置，跳过恢复",
			logging.String("projection", projectionName))
		return pm.StartProjection(projectionName)
	}

	// 加载检查点
	checkpoint, err := checkpointStore.Load(ctx, projectionName)
	if err != nil {
		if err == ErrCheckpointNotFound {
			projectionLogger.Info(ctx, "检查点不存在，从头开始",
				logging.String("projection", projectionName))
			checkpoint = NewCheckpoint(projectionName, 0, "", time.Time{})
		} else {
			return fmt.Errorf("failed to load checkpoint: %w", err)
		}
	}

	projectionLogger.Info(ctx, "从检查点恢复投影",
		logging.String("projection", projectionName),
		logging.Int64("position", checkpoint.Position),
		logging.String("last_event_id", checkpoint.LastEventID))

	// 用检查点预填充状态，避免重放前状态缺失
	pm.mutex.Lock()
	if status, ok := pm.statuses[projectionName]; ok && status != nil {
		status.LastEventID = checkpoint.LastEventID
		status.LastEventTime = checkpoint.LastEventTime
		status.ProcessedEvents = checkpoint.Position
		status.Status = "stopped"
		status.LastError = ""
		status.UpdatedAt = time.Now()
	}
	pm.mutex.Unlock()

	if eventStore == nil {
		projectionLogger.Warn(ctx, "事件存储未配置，无法从检查点重放，直接启动",
			logging.String("projection", projectionName))
		return pm.StartProjection(projectionName)
	}

	replayed, err := pm.replayProjectionFromCheckpoint(ctx, projectionName, projection, checkpoint)
	if err != nil {
		return err
	}

	projectionLogger.Info(ctx, "从检查点恢复并完成历史回放",
		logging.String("projection", projectionName),
		logging.Int64("replayed_events", replayed))

	return pm.StartProjection(projectionName)
}

func (pm *ProjectionManager) replayProjectionFromCheckpoint(ctx context.Context, projectionName string, projection IProjection, checkpoint *Checkpoint) (int64, error) {
	supported := make(map[string]struct{})
	for _, t := range projection.GetSupportedEventTypes() {
		supported[t] = struct{}{}
	}

	lastEventID := checkpoint.LastEventID
	fromTime := checkpoint.LastEventTime
	var replayed int64

	for {
		events, hasMore, err := pm.fetchEventsForReplay(ctx, lastEventID, fromTime)
		if err != nil {
			return replayed, fmt.Errorf("failed to load events for projection %s: %w", projectionName, err)
		}
		if len(events) == 0 {
			if hasMore {
				continue
			}
			break
		}

		for i := range events {
			evt := &events[i]
			if len(supported) > 0 {
				if _, ok := supported[evt.GetType()]; !ok {
					continue
				}
			}

			if err := pm.applyReplayEvent(ctx, projectionName, projection, evt); err != nil {
				pm.mutex.Lock()
				if status, ok := pm.statuses[projectionName]; ok && status != nil {
					status.Status = "error"
					status.LastError = err.Error()
					status.UpdatedAt = time.Now()
				}
				pm.mutex.Unlock()

				return replayed, fmt.Errorf("replay projection %s failed at event %s: %w", projectionName, evt.GetID(), err)
			}

			replayed++
			lastEventID = evt.GetID()
			fromTime = evt.GetTimestamp()
		}

		if !hasMore {
			break
		}
	}

	return replayed, nil
}

func (pm *ProjectionManager) fetchEventsForReplay(ctx context.Context, after string, fromTime time.Time) ([]eventing.Event, bool, error) {
	if extended, ok := pm.eventStore.(store.IEventStoreExtended); ok {
		stream, err := extended.GetEventStreamWithCursor(ctx, &store.StreamOptions{
			After:    after,
			FromTime: fromTime,
		})
		if err != nil {
			return nil, false, err
		}
		if stream == nil {
			return nil, false, nil
		}
		return stream.Events, stream.HasMore, nil
	}

	events, err := pm.eventStore.StreamEvents(ctx, fromTime)
	if err != nil {
		return nil, false, err
	}

	filtered := store.FilterEventsWithOptions(events, &store.StreamOptions{
		After:    after,
		FromTime: fromTime,
	})
	if filtered == nil {
		return nil, false, nil
	}
	return filtered.Events, filtered.HasMore, nil
}

func (pm *ProjectionManager) applyReplayEvent(ctx context.Context, projectionName string, projection IProjection, evt eventing.IEvent) error {
	checkpointStore := pm.checkpointStore

	err := projection.Handle(ctx, evt)

	var (
		statusCopy       *ProjectionStatus
		processedEvents  int64
		lastEventID      string
		lastEventTime    time.Time
		checkpointNeeded bool
	)

	pm.mutex.Lock()
	if status, ok := pm.statuses[projectionName]; ok && status != nil {
		now := time.Now()
		if err != nil {
			status.FailedEvents++
			status.LastError = err.Error()
		} else {
			status.ProcessedEvents++
			status.LastEventID = evt.GetID()
			status.LastEventTime = evt.GetTimestamp()
			status.LastError = ""

			processedEvents = status.ProcessedEvents
			lastEventID = status.LastEventID
			lastEventTime = status.LastEventTime
			checkpointNeeded = true
		}
		status.UpdatedAt = now
		statusCopy = status
	}
	pm.mutex.Unlock()

	if err != nil {
		return err
	}

	if checkpointStore != nil && checkpointNeeded && statusCopy != nil {
		if saveErr := checkpointStore.Save(ctx, NewCheckpoint(projectionName, processedEvents, lastEventID, lastEventTime)); saveErr != nil {
			projectionLogger.Warn(ctx, "保存检查点失败", logging.Error(saveErr),
				logging.String("projection", projectionName))
		}
	}

	return nil
}

// ResumeAllFromCheckpoint 从检查点恢复所有投影
//
// 批量恢复所有已注册的投影。
//
// 参数：
//   - ctx: 上下文
//
// 返回：
//   - error: 恢复失败错误
func (pm *ProjectionManager) ResumeAllFromCheckpoint(ctx context.Context) error {
	pm.mutex.RLock()
	names := make([]string, 0, len(pm.projections))
	for name := range pm.projections {
		names = append(names, name)
	}
	pm.mutex.RUnlock()

	for _, name := range names {
		if err := pm.ResumeFromCheckpoint(ctx, name); err != nil {
			projectionLogger.Error(ctx, "恢复投影失败", logging.Error(err),
				logging.String("projection", name))
			// 继续恢复其他投影
		}
	}

	return nil
}

// RegisterProjection 注册投影
func (pm *ProjectionManager) RegisterProjection(projection IProjection) error {
	return pm.RegisterProjectionWithContext(context.Background(), projection)
}

// RegisterProjectionWithContext 注册投影（支持上下文透传）
func (pm *ProjectionManager) RegisterProjectionWithContext(ctx context.Context, projection IProjection) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if projection == nil {
		return fmt.Errorf("projection cannot be nil")
	}

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	name := projection.GetName()
	if _, exists := pm.projections[name]; exists {
		return fmt.Errorf("projection %s already registered", name)
	}
	if name == "" {
		return fmt.Errorf("projection name cannot be empty")
	}

	pm.projections[name] = projection
	pm.statuses[name] = &ProjectionStatus{
		Name:      name,
		Status:    "stopped",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if pm.handlers[name] == nil {
		pm.handlers[name] = make(map[string]*projectionEventHandler)
	}

	subscribedHandlers := make(map[string]*projectionEventHandler)
	for _, eventType := range projection.GetSupportedEventTypes() {
		handler := &projectionEventHandler{projection: projection, manager: pm}
		pm.handlers[name][eventType] = handler

		if err := pm.eventBus.SubscribeEvent(ctx, eventType, handler); err != nil {
			// 回滚当前投影的注册状态，避免半注册
			delete(pm.projections, name)
			delete(pm.statuses, name)
			delete(pm.handlers, name)
			for t, h := range subscribedHandlers {
				if unSubErr := pm.eventBus.UnsubscribeEvent(ctx, t, h); unSubErr != nil {
					projectionLogger.Warn(ctx, "订阅回滚取消订阅失败", logging.Error(unSubErr),
						logging.String("projection", name), logging.String("event_type", t))
				}
			}
			return fmt.Errorf("failed to subscribe to event type %s: %w", eventType, err)
		}
		subscribedHandlers[eventType] = handler
	}

	projectionLogger.Info(ctx, "[ProjectionManager] 注册投影", logging.String("projection", name))
	return nil
}

// UnregisterProjection 取消注册投影
func (pm *ProjectionManager) UnregisterProjection(name string) error {
	return pm.UnregisterProjectionWithContext(context.Background(), name)
}

// UnregisterProjectionWithContext 取消注册投影（支持上下文透传）
func (pm *ProjectionManager) UnregisterProjectionWithContext(ctx context.Context, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	projection, exists := pm.projections[name]
	if !exists {
		return fmt.Errorf("projection %s not found", name)
	}

	for _, eventType := range projection.GetSupportedEventTypes() {
		var handler *projectionEventHandler
		if pm.handlers[name] != nil {
			handler = pm.handlers[name][eventType]
		}
		if handler == nil {
			projectionLogger.Warn(ctx, "[ProjectionManager] 找不到已注册的处理器实例，可能无法正确取消订阅",
				logging.String("projection", name),
				logging.String("event_type", eventType),
			)
		}

		if err := pm.eventBus.UnsubscribeEvent(ctx, eventType, handler); err != nil {
			projectionLogger.Warn(ctx, "取消订阅事件失败", logging.Error(err),
				logging.String("event_type", eventType),
				logging.String("projection", name),
			)
		}

		if pm.handlers[name] != nil {
			delete(pm.handlers[name], eventType)
		}
	}

	delete(pm.projections, name)
	delete(pm.statuses, name)
	delete(pm.handlers, name)

	projectionLogger.Info(ctx, "[ProjectionManager] 取消注册投影", logging.String("projection", name))
	return nil
}

// StartProjection 启动投影
func (pm *ProjectionManager) StartProjection(name string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	status, exists := pm.statuses[name]
	if !exists {
		return fmt.Errorf("projection %s not found", name)
	}

	if status.Status == "running" {
		return nil
	}

	status.Status = "running"
	status.UpdatedAt = time.Now()

	projectionLogger.Info(context.Background(), "[ProjectionManager] 启动投影", logging.String("projection", name))
	return nil
}

// StopProjection 停止投影
func (pm *ProjectionManager) StopProjection(name string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	status, exists := pm.statuses[name]
	if !exists {
		return fmt.Errorf("projection %s not found", name)
	}

	if status.Status == "stopped" {
		return nil
	}

	status.Status = "stopped"
	status.UpdatedAt = time.Now()

	projectionLogger.Info(context.Background(), "[ProjectionManager] 停止投影", logging.String("projection", name))
	return nil
}

// GetProjectionStatus 获取投影状态
func (pm *ProjectionManager) GetProjectionStatus(name string) (*ProjectionStatus, error) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	status, exists := pm.statuses[name]
	if !exists {
		return nil, fmt.Errorf("projection %s not found", name)
	}

	// 返回状态副本，避免调用方在无锁情况下读写共享状态导致竞态
	statusCopy := *status
	return &statusCopy, nil
}

// GetAllProjectionStatuses 获取所有投影状态
func (pm *ProjectionManager) GetAllProjectionStatuses() map[string]*ProjectionStatus {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	result := make(map[string]*ProjectionStatus, len(pm.statuses))
	for name, status := range pm.statuses {
		statusCopy := *status
		result[name] = &statusCopy
	}

	return result
}

// RebuildProjection 重建投影
func (pm *ProjectionManager) RebuildProjection(ctx context.Context, name string, events []eventing.Event) error {
	pm.mutex.Lock()
	checkpointStore := pm.checkpointStore
	projection, exists := pm.projections[name]
	status := pm.statuses[name]
	pm.mutex.Unlock()

	if !exists {
		return fmt.Errorf("projection %s not found", name)
	}

	projectionLogger.Info(ctx, "开始重建投影",
		logging.String("projection", name),
		logging.Int("events", len(events)))

	// 清空检查点（如果已配置）
	if checkpointStore != nil {
		if err := checkpointStore.Delete(ctx, name); err != nil {
			projectionLogger.Warn(ctx, "删除检查点失败", logging.Error(err),
				logging.String("projection", name))
			// 继续重建
		}
	}

	pm.mutex.Lock()
	status.Status = "rebuilding"
	status.UpdatedAt = time.Now()
	pm.mutex.Unlock()

	if err := projection.Rebuild(ctx, events); err != nil {
		pm.mutex.Lock()
		status.Status = "error"
		status.LastError = err.Error()
		status.UpdatedAt = time.Now()
		pm.mutex.Unlock()
		return fmt.Errorf("failed to rebuild projection %s: %w", name, err)
	}

	pm.mutex.Lock()
	status.Status = "stopped"
	status.ProcessedEvents = int64(len(events))
	status.UpdatedAt = time.Now()
	pm.mutex.Unlock()

	// 保存新的检查点
	if checkpointStore != nil && len(events) > 0 {
		lastEvent := events[len(events)-1]
		checkpoint := NewCheckpoint(
			name,
			int64(len(events)),
			lastEvent.ID,
			lastEvent.Timestamp,
		)

		if err := checkpointStore.Save(ctx, checkpoint); err != nil {
			projectionLogger.Warn(ctx, "保存检查点失败", logging.Error(err),
				logging.String("projection", name))
		}
	}

	projectionLogger.Info(ctx, "重建投影完成",
		logging.String("projection", name),
		logging.Int("events", len(events)))
	return nil
}

// projectionEventHandler 投影事件处理器
type projectionEventHandler struct {
	projection IProjection
	manager    *ProjectionManager
}

// HandleEvent 处理事件
func (h *projectionEventHandler) HandleEvent(ctx context.Context, event eventing.IEvent) error {
	name := h.projection.GetName()

	// 首先在读锁下检查投影是否存在且处于运行状态，并捕获当前 checkpointStore 引用
	h.manager.mutex.RLock()
	status, exists := h.manager.statuses[name]
	checkpointStore := h.manager.checkpointStore
	shouldProcess := exists && status.Status == "running"
	h.manager.mutex.RUnlock()

	if !shouldProcess {
		return nil
	}

	// 在不持锁的情况下处理事件，避免长时间占用管理器锁
	err := h.projection.Handle(ctx, event)

	var (
		processedEvents int64
		failedEvents    int64
		checkpoint      *Checkpoint
	)

	// 根据处理结果更新状态，需要在写锁下进行以避免与其他操作（Start/Stop/Unregister）产生竞态
	h.manager.mutex.Lock()
	status, exists = h.manager.statuses[name]
	if exists {
		now := time.Now()
		if err != nil {
			status.FailedEvents++
			status.LastError = err.Error()
			status.UpdatedAt = now
		} else {
			status.ProcessedEvents++
			status.LastEventID = event.GetID()
			status.LastEventTime = event.GetTimestamp()
			status.UpdatedAt = now

			if checkpointStore != nil {
				checkpoint = NewCheckpoint(
					name,
					status.ProcessedEvents,
					event.GetID(),
					event.GetTimestamp(),
				)
			}
		}
		processedEvents = status.ProcessedEvents
		failedEvents = status.FailedEvents
	}
	h.manager.mutex.Unlock()

	if err != nil {
		// 如果 event 是 eventing.Event 类型，则传递给 DeadLetterFunc
		if e, ok := event.(*eventing.Event); ok {
			h.manager.config.DeadLetterFunc(err, *e, name)
		}

		// 使用快照后的计数值进行日志记录，避免在无锁状态下访问共享状态
		projectionLogger.Error(ctx, "投影处理事件失败", logging.Error(err),
			logging.String("projection", name),
			logging.String("event_type", event.GetType()),
			logging.Int64("processed_events", processedEvents),
			logging.Int64("failed_events", failedEvents),
		)
		return err
	}

	// 自动保存检查点（如果已配置）
	if checkpoint != nil && checkpointStore != nil {
		if err := checkpointStore.Save(ctx, checkpoint); err != nil {
			projectionLogger.Warn(ctx, "保存检查点失败", logging.Error(err),
				logging.String("projection", name))
			// 不中断事件处理
		}
	}

	projectionLogger.Debug(ctx, "投影处理事件成功",
		logging.String("event_type", event.GetType()),
		logging.String("projection", name),
	)
	return nil
}

// GetEventTypes 获取支持的事件类型
func (h *projectionEventHandler) GetEventTypes() []string {
	return h.projection.GetSupportedEventTypes()
}

// GetHandlerName 获取处理器名称
func (h *projectionEventHandler) GetHandlerName() string {
	return h.projection.GetName()
}

// Handle 实现IMessageHandler接口
func (h *projectionEventHandler) Handle(ctx context.Context, message messaging.IMessage) error {
	// 尝试将message转换为 eventing.IEvent
	if event, ok := message.(eventing.IEvent); ok {
		return h.HandleEvent(ctx, event)
	}
	return fmt.Errorf("invalid message type: %T", message)
}

// Type 返回处理器类型
func (h *projectionEventHandler) Type() string {
	return "projectionEventHandler"
}

// Ensure this implements the eventbus.EventHandler interface
var _ bus.IEventHandler = (*projectionEventHandler)(nil)
