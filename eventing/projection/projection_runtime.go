package projection

import (
	"context"
	"sync"
	"time"

	"gochen/eventing"
)

type projectionRuntime[ID comparable] struct {
	projection IProjection[ID]
	status     *ProjectionStatus
	handlers   map[string]*projectionEventHandler[ID]
	checkpoint checkpointState
	cursor     *Checkpoint
	active     bool
	cleanupErr error

	recentEventIDs   map[string]struct{}
	recentEventOrder []string

	execMu    runtimeMutex // 串行处理、恢复和重建；清理可取消等待。
	stateMu   sync.RWMutex // 保护 status、active、cleanupErr、cursor 和 checkpoint。
	cleanupMu runtimeMutex // 串行注销；注册发布后保护 handlers。

	// lastCatchUpAt 记录最近一次空洞追赶的发起时刻（受 stateMu 保护）。
	lastCatchUpAt time.Time
	// catchUpPending 标记上一轮追赶因存储故障（非 NotFound）失败且尚未成功
	// 重试成功（受 stateMu 保护）。置位期间追赶重试绕过节流窗口。
	catchUpPending bool
}

// runtimeMutex 用容量为 1 的 channel 实现可取消等待；必须在运行期构造时初始化。
type runtimeMutex chan struct{}

func (m runtimeMutex) Lock() { m <- struct{}{} }

func (m runtimeMutex) Unlock() { <-m }

func (m runtimeMutex) lockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m <- struct{}{}:
		// 同时发生解锁和取消时，不带着已取消的上下文继续清理。
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newProjectionRuntime[ID comparable](projection IProjection[ID]) *projectionRuntime[ID] {
	now := time.Now()
	return &projectionRuntime[ID]{
		projection: projection,
		status: &ProjectionStatus{
			Name:      projection.Name(),
			Status:    "stopped",
			CreatedAt: now,
			UpdatedAt: now,
		},
		handlers: make(map[string]*projectionEventHandler[ID]),
		checkpoint: checkpointState{
			lastSaveTime:        now,
			eventsSinceLastSave: 0,
		},
		recentEventIDs:   make(map[string]struct{}),
		recentEventOrder: make([]string, 0, 128),
		active:           true,
		execMu:           make(runtimeMutex, 1),
		cleanupMu:        make(runtimeMutex, 1),
	}
}

func (rt *projectionRuntime[ID]) statusCopy() *ProjectionStatus {
	if rt == nil {
		return nil
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	if rt.status == nil {
		return nil
	}
	cp := *rt.status
	// 清理状态与错误优先于仍在退出的处理/重建更新。
	if !rt.active {
		cp.Status = "cleanup_pending"
		if rt.cleanupErr != nil {
			cp.LastError = rt.cleanupErr.Error()
		}
	}
	return &cp
}

func (rt *projectionRuntime[ID]) isRunning() bool {
	_, running := rt.lifecycleState()
	return running
}

func (rt *projectionRuntime[ID]) isActive() bool {
	if rt == nil {
		return false
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	return rt.active
}

// lifecycleState 在同一把读锁下返回 active 与 running 快照。
func (rt *projectionRuntime[ID]) lifecycleState() (active bool, running bool) {
	if rt == nil {
		return false, false
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	return rt.active, rt.active && rt.status != nil && rt.status.Status == "running"
}

func (rt *projectionRuntime[ID]) processedEvents() int64 {
	if rt == nil {
		return 0
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	if rt.status == nil {
		return 0
	}
	return rt.status.ProcessedEvents
}

func (rt *projectionRuntime[ID]) markStopped() {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	if !rt.active {
		return
	}
	rt.status.Status = "stopped"
	rt.status.UpdatedAt = time.Now()
}

func (rt *projectionRuntime[ID]) markRunning() {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	if !rt.active {
		return
	}
	rt.status.Status = "running"
	rt.status.UpdatedAt = time.Now()
}

func (rt *projectionRuntime[ID]) deactivate() {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.active = false
	rt.status.Status = "cleanup_pending"
	rt.status.UpdatedAt = time.Now()
}

func (rt *projectionRuntime[ID]) recordCleanupError(err error) {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.cleanupErr = err
	rt.status.UpdatedAt = time.Now()
}

func (rt *projectionRuntime[ID]) markRebuilding() {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.status.Status = "rebuilding"
	rt.status.UpdatedAt = time.Now()
}

func (rt *projectionRuntime[ID]) isRebuilding() bool {
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	return rt.status.Status == "rebuilding"
}

func (rt *projectionRuntime[ID]) markError(err error) {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.status.Status = "error"
	if err != nil {
		rt.status.LastError = err.Error()
	}
	rt.status.UpdatedAt = time.Now()
}

func (rt *projectionRuntime[ID]) prefillFromCheckpoint(checkpoint *Checkpoint) {
	if checkpoint == nil {
		return
	}
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.status.LastEventID = checkpoint.LastEventID
	rt.status.LastEventTime = checkpoint.LastEventTime
	rt.status.ProcessedEvents = checkpoint.Position
	rt.status.Status = "stopped"
	rt.status.LastError = ""
	rt.status.UpdatedAt = time.Now()
	rt.cursor = checkpoint.Clone()
	rt.checkpoint.lastSaveTime = time.Now()
	rt.checkpoint.eventsSinceLastSave = 0
	if checkpoint.LastEventID != "" {
		rt.recordProcessedEventIDUnsafe(checkpoint.LastEventID)
	}
}

func (rt *projectionRuntime[ID]) updateAfterRebuild(events []eventing.Event[ID], position int64) {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()

	rt.status.Status = "stopped"
	rt.status.ProcessedEvents = int64(len(events))
	if len(events) > 0 {
		lastEvent := events[len(events)-1]
		rt.status.LastEventID = lastEvent.ID
		rt.status.LastEventTime = lastEvent.Timestamp
		for _, e := range events {
			rt.recordProcessedEventIDUnsafe(e.ID)
		}
	} else {
		rt.status.LastEventID = ""
		rt.status.LastEventTime = time.Time{}
	}
	rt.status.UpdatedAt = time.Now()
	// 运行期游标必须与持久化 checkpoint 对齐：使用重建后的全局位置，
	// 避免内存游标停留在 len(events) 而首条在线事件被误判为位置空洞。
	rt.cursor = NewCheckpoint(rt.status.Name, position, rt.status.LastEventID, rt.status.LastEventTime)
	rt.checkpoint.lastSaveTime = time.Now()
	rt.checkpoint.eventsSinceLastSave = 0
}

func (rt *projectionRuntime[ID]) cursorSnapshot() *Checkpoint {
	if rt == nil {
		return nil
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	if rt.cursor == nil {
		return nil
	}
	return rt.cursor.Clone()
}

func (rt *projectionRuntime[ID]) shouldSaveCheckpointAfterEvent(config *ProjectionConfig) bool {
	if rt == nil {
		return true
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	return shouldSaveCheckpointAfterEventState(rt.checkpoint, config)
}

func (rt *projectionRuntime[ID]) recordApplyResult(
	evt eventing.IEvent,
	err error,
	clearLastErrorOnSuccess bool,
	nextCursor *Checkpoint,
	checkpointSaved bool,
) applyEventCommonResult {
	var res applyEventCommonResult
	if rt == nil {
		return res
	}
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()

	if rt.status == nil {
		return res
	}
	now := time.Now()
	if err != nil {
		rt.status.FailedEvents++
		rt.status.LastError = err.Error()
	} else {
		rt.status.ProcessedEvents++
		rt.status.LastEventID = evt.GetID()
		rt.status.LastEventTime = evt.GetTimestamp()
		if clearLastErrorOnSuccess {
			rt.status.LastError = ""
		}
		if nextCursor != nil {
			// 单调性防护：乱序/迟到事件携带的回退游标不应倒退内存游标，
			// 否则会连带后续事件被误判空洞而频繁假追赶。
			if rt.cursor == nil || nextCursor.Position >= rt.cursor.Position {
				rt.cursor = nextCursor.Clone()
			}
		}
		updateCheckpointTrackerState(&rt.checkpoint, checkpointSaved)
		rt.recordProcessedEventIDUnsafe(evt.GetID())
	}
	rt.status.UpdatedAt = now
	res.processedEvents = rt.status.ProcessedEvents
	res.failedEvents = rt.status.FailedEvents
	return res
}

func (rt *projectionRuntime[ID]) recordProcessedEventIDUnsafe(id string) {
	if id == "" {
		return
	}
	if rt.recentEventIDs == nil {
		rt.recentEventIDs = make(map[string]struct{})
	}
	if _, exists := rt.recentEventIDs[id]; exists {
		return
	}
	const maxRecentEvents = 10000
	if len(rt.recentEventOrder) >= maxRecentEvents {
		oldest := rt.recentEventOrder[0]
		rt.recentEventOrder = rt.recentEventOrder[1:]
		delete(rt.recentEventIDs, oldest)
	}
	rt.recentEventIDs[id] = struct{}{}
	rt.recentEventOrder = append(rt.recentEventOrder, id)
}

func (rt *projectionRuntime[ID]) hasProcessedEventID(id string) bool {
	if rt == nil || id == "" {
		return false
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	if rt.recentEventIDs == nil {
		return false
	}
	_, exists := rt.recentEventIDs[id]
	return exists
}

// canStartCatchUp 判断是否允许发起新一轮空洞追赶（节流窗口控制）。
// 冷启动（cursor == nil）不受窗口限制。minInterval <= 0 表示不节流。
func (rt *projectionRuntime[ID]) canStartCatchUp(now time.Time, minInterval time.Duration) bool {
	if rt == nil || minInterval <= 0 {
		return true
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	return rt.lastCatchUpAt.IsZero() || now.Sub(rt.lastCatchUpAt) >= minInterval
}

// markCatchUp 记录一次追赶发起时刻（仅在追赶成功或已降级对齐时调用）。
func (rt *projectionRuntime[ID]) markCatchUp(now time.Time) {
	if rt == nil {
		return
	}
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.lastCatchUpAt = now
}

// markCatchUpFailed 标记上一轮追赶失败待重试。
// 追赶失败绝不能把运行状态置为终态 error（那会让后续追赶因状态非 running
// 空转、在线事件被 requireRunning 静默跳过，投影永久卡死）；这里仅记录
// “追赶未完成”，由处理路径在下一个事件上立即重试（绕过节流窗口）。
func (rt *projectionRuntime[ID]) markCatchUpFailed() {
	if rt == nil {
		return
	}
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.catchUpPending = true
}

// hasPendingCatchUp 报告是否存在待重试的失败追赶。
func (rt *projectionRuntime[ID]) hasPendingCatchUp() bool {
	if rt == nil {
		return false
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	return rt.catchUpPending
}

// clearPendingCatchUp 在追赶成功（或 NotFound 降级放弃历史）后清除待重试标记。
func (rt *projectionRuntime[ID]) clearPendingCatchUp() {
	if rt == nil {
		return
	}
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.catchUpPending = false
}

// alignCursorToEvent 在 checkpoint gap（游标引用的历史事件已被清理）时
// 把内存游标对齐到当前在线事件，表示放弃历史追赶。
// 仅在游标未超前于该事件位置时对齐（保持单调性）。
func (rt *projectionRuntime[ID]) alignCursorToEvent(evt eventing.IEvent) {
	if rt == nil || evt == nil || rt.status == nil {
		return
	}
	position := int64(0)
	if positioned, ok := evt.(interface{ GetGlobalPosition() int64 }); ok {
		if pos := positioned.GetGlobalPosition(); pos > 0 {
			position = pos
		}
	}
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	if rt.cursor != nil && rt.cursor.Position > position {
		return
	}
	rt.cursor = NewCheckpoint(rt.status.Name, position, evt.GetID(), evt.GetTimestamp())
	rt.checkpoint.lastSaveTime = time.Now()
	rt.checkpoint.eventsSinceLastSave = 0
}
