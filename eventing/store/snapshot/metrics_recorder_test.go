package snapshot

import (
	"testing"
	"time"
)

type testSnapshotRecorderA struct {
	created int
	loaded  int
}

func (r *testSnapshotRecorderA) RecordSnapshotCreated(_ time.Duration)        { r.created++ }
func (r *testSnapshotRecorderA) RecordSnapshotLoaded(_ time.Duration, _ bool) { r.loaded++ }

type testSnapshotRecorderB struct{}

func (r testSnapshotRecorderB) RecordSnapshotCreated(_ time.Duration)        {}
func (r testSnapshotRecorderB) RecordSnapshotLoaded(_ time.Duration, _ bool) {}

func TestSnapshotManager_SetMetricsRecorder_AllowsNilAndDifferentTypes(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected no panic, got %v", rec)
		}
	}()

	sm := &Manager[int64]{}
	a := &testSnapshotRecorderA{}

	sm.SetMetricsRecorder(a)
	if got := sm.getMetrics(); got == nil {
		t.Fatalf("expected non-nil recorder")
	}

	// Switching implementations should never panic.
	sm.SetMetricsRecorder(testSnapshotRecorderB{})
	sm.SetMetricsRecorder(nil)
	sm.SetMetricsRecorder(a)
}
