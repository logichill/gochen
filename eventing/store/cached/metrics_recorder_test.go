package cached

import "testing"

type testCacheRecorderA struct{ hits int }

func (r *testCacheRecorderA) RecordCacheHit()      { r.hits++ }
func (r *testCacheRecorderA) RecordCacheMiss()     {}
func (r *testCacheRecorderA) RecordCacheEviction() {}

type testCacheRecorderB struct{}

func (r testCacheRecorderB) RecordCacheHit()      {}
func (r testCacheRecorderB) RecordCacheMiss()     {}
func (r testCacheRecorderB) RecordCacheEviction() {}

func TestCachedEventStore_SetMetricsRecorder_AllowsNilAndDifferentTypes(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("expected no panic, got %v", rec)
		}
	}()

	s := &CachedEventStore[int64]{}
	a := &testCacheRecorderA{}

	s.SetMetricsRecorder(a)
	if got := s.getMetrics(); got == nil {
		t.Fatalf("expected non-nil recorder")
	}

	// Switching implementations should never panic.
	s.SetMetricsRecorder(testCacheRecorderB{})
	s.SetMetricsRecorder(nil)
	s.SetMetricsRecorder(a)
}
