package projection

import "time"

func (pm *ProjectionManager[ID]) shouldSaveCheckpointAfterEvent(rt *projectionRuntime[ID]) bool {
	if rt == nil {
		return true
	}
	return rt.shouldSaveCheckpointAfterEvent(pm.config)
}

func shouldSaveCheckpointAfterEventState(checkpoint checkpointState, config *ProjectionConfig) bool {
	if config == nil {
		return true
	}
	countThreshold := config.CheckpointSaveCount
	intervalThreshold := config.CheckpointSaveInterval

	if countThreshold <= 0 && intervalThreshold <= 0 {
		return true
	}
	if countThreshold > 0 && checkpoint.eventsSinceLastSave+1 >= countThreshold {
		return true
	}
	if intervalThreshold > 0 && time.Since(checkpoint.lastSaveTime) >= intervalThreshold {
		return true
	}
	return false
}

func updateCheckpointTrackerState(checkpoint *checkpointState, saved bool) {
	if checkpoint == nil {
		return
	}
	if saved {
		checkpoint.lastSaveTime = time.Now()
		checkpoint.eventsSinceLastSave = 0
	} else {
		checkpoint.eventsSinceLastSave++
	}
}
