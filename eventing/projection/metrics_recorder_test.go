package projection

import (
	"testing"
	"time"
)

type testProjectionRecorderA struct{ processed int }

func (r *testProjectionRecorderA) RecordEventProcessed(_ time.Duration, _ bool)   { r.processed++ }
func (r *testProjectionRecorderA) RecordProjectionUpdate(_ bool, _ time.Duration) {}

type testProjectionRecorderB struct{}

func (r testProjectionRecorderB) RecordEventProcessed(_ time.Duration, _ bool)   {}
func (r testProjectionRecorderB) RecordProjectionUpdate(_ bool, _ time.Duration) {}

func TestProjectionManager_SetMetricsRecorder_AllowsNilAndDifferentTypes(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected no panic, got %v", rec)
		}
	}()

	pm := &ProjectionManager[int64]{}

	a := &testProjectionRecorderA{}
	pm.SetMetricsRecorder(a)
	if got := pm.getMetrics(); got == nil {
		t.Fatalf("expected non-nil recorder")
	}

	// Switching implementations should never panic.
	pm.SetMetricsRecorder(testProjectionRecorderB{})
	pm.SetMetricsRecorder(nil)
	pm.SetMetricsRecorder(a)
}
