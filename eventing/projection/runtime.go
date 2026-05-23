package projection

import (
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

	execMu  sync.Mutex
	stateMu sync.RWMutex
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
		active: true,
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
	return &cp
}

func (rt *projectionRuntime[ID]) isRunning() bool {
	if rt == nil {
		return false
	}
	rt.stateMu.RLock()
	defer rt.stateMu.RUnlock()
	return rt.active && rt.status != nil && rt.status.Status == "running"
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
	rt.status.Status = "stopped"
	rt.status.UpdatedAt = time.Now()
}

func (rt *projectionRuntime[ID]) markRebuilding() {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()
	rt.status.Status = "rebuilding"
	rt.status.UpdatedAt = time.Now()
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
}

func (rt *projectionRuntime[ID]) updateAfterRebuild(events []eventing.Event[ID]) {
	rt.stateMu.Lock()
	defer rt.stateMu.Unlock()

	rt.status.Status = "stopped"
	rt.status.ProcessedEvents = int64(len(events))
	if len(events) > 0 {
		lastEvent := events[len(events)-1]
		rt.status.LastEventID = lastEvent.ID
		rt.status.LastEventTime = lastEvent.Timestamp
	} else {
		rt.status.LastEventID = ""
		rt.status.LastEventTime = time.Time{}
	}
	rt.status.UpdatedAt = time.Now()
	rt.cursor = NewCheckpoint(rt.status.Name, rt.status.ProcessedEvents, rt.status.LastEventID, rt.status.LastEventTime)
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
			rt.cursor = nextCursor.Clone()
		}
		updateCheckpointTrackerState(&rt.checkpoint, checkpointSaved)
	}
	rt.status.UpdatedAt = now
	res.processedEvents = rt.status.ProcessedEvents
	res.failedEvents = rt.status.FailedEvents
	return res
}
