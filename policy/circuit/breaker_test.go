package circuit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/clock"
	"gochen/errors"
)

func TestBreaker_WindowMode_TripsOnIntermittentFailures(t *testing.T) {
	b := New(Config{
		WindowSize:           10,
		FailureRateThreshold: 0.5,
		MinimumRequests:      4,
		ResetTimeout:         time.Minute,
	})

	// 交替成功/失败：连续失败计数模式永不跳闸，但窗口失败率达到 50% 应跳闸。
	fail := errors.New("boom")
	for i := 0; i < 8; i++ {
		if i%2 == 0 {
			_ = b.Call(func() error { return fail })
		} else {
			_ = b.Call(func() error { return nil })
		}
	}

	err := b.Call(func() error { return nil })
	if !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("expected breaker open under intermittent failures, got %v", err)
	}
}

func TestBreaker_WindowMode_StaysClosedBelowThreshold(t *testing.T) {
	b := New(Config{
		WindowSize:           10,
		FailureRateThreshold: 0.5,
		MinimumRequests:      4,
		ResetTimeout:         time.Minute,
	})

	fail := errors.New("boom")
	// 2 失败 / 10 成功，且失败分散，任一评估点失败率都 < 50%，应保持闭合放行。
	results := []bool{true, true, true, true, false, true, true, false, true, true}
	for _, ok := range results {
		_ = b.Call(func() error {
			if ok {
				return nil
			}
			return fail
		})
	}

	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("expected breaker to stay closed below threshold, got %v", err)
	}
}

func TestBreaker_WindowMode_TripsOnSuccessStampsOpenTime(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	b := New(Config{
		WindowSize:           4,
		FailureRateThreshold: 0.5,
		MinimumRequests:      4,
		ResetTimeout:         time.Minute,
		Clock:                clk,
	})

	fail := errors.New("boom")
	// 3 次失败分散在超过 ResetTimeout 的时间跨度上。
	for i := 0; i < 3; i++ {
		_ = b.Call(func() error { return fail })
		clk.Advance(30 * time.Second)
	}
	// 第 4 次为成功调用：补齐窗口并触发跳闸（失败率 3/4 >= 0.5）。
	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("trip-triggering call should propagate its own result, got %v", err)
	}

	// 跳闸时刻起算冷却窗口：此刻立即调用应被拒绝。
	if err := b.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("expected breaker open immediately after trip, got %v", err)
	}

	// 推进到接近但未达 ResetTimeout，仍应拒绝（证明 lastFailTime 取的是跳闸时刻而非更早失败）。
	clk.Advance(59 * time.Second)
	if err := b.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("expected breaker still open before ResetTimeout elapses, got %v", err)
	}

	// 越过 ResetTimeout 后允许一次半开试探。
	clk.Advance(2 * time.Second)
	probed := false
	if err := b.Call(func() error { probed = true; return nil }); err != nil {
		t.Fatalf("expected half-open probe to run after ResetTimeout, got %v", err)
	}
	if !probed {
		t.Fatal("expected probe fn to execute in half-open")
	}
}

func TestBreaker_HalfOpen_AllowsOnlyOneProbe(t *testing.T) {
	b := New(Config{
		MaxFailures:  1,
		ResetTimeout: 10 * time.Millisecond,
	})

	// 1) Force Open
	_ = b.Call(func() error { return errors.New("boom") })

	// 2) Wait for ResetTimeout so Open -> HalfOpen can happen.
	time.Sleep(2 * b.cfg.ResetTimeout)

	probeStarted := make(chan struct{})
	probeRelease := make(chan struct{})
	var probeCalls int32

	probeErrCh := make(chan error, 1)
	go func() {
		probeErrCh <- b.Call(func() error {
			if atomic.AddInt32(&probeCalls, 1) == 1 {
				close(probeStarted)
			}
			<-probeRelease
			return nil
		})
	}()

	select {
	case <-probeStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("probe did not start in time")
	}

	const contenders = 20
	errs := make(chan error, contenders)
	var contenderCalls int32
	var wg sync.WaitGroup
	wg.Add(contenders)
	for i := 0; i < contenders; i++ {
		go func() {
			defer wg.Done()
			errs <- b.Call(func() error {
				atomic.AddInt32(&contenderCalls, 1)
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)

	close(probeRelease)

	if err := <-probeErrCh; err != nil {
		t.Fatalf("probe call failed: %v", err)
	}

	if got := atomic.LoadInt32(&probeCalls); got != 1 {
		t.Fatalf("expected exactly 1 probe execution, got %d", got)
	}
	if got := atomic.LoadInt32(&contenderCalls); got != 0 {
		t.Fatalf("expected contenders not to execute fn in half-open, got %d", got)
	}

	for err := range errs {
		if err == nil || !errors.Is(err, errors.ServiceUnavailable) {
			t.Fatalf("expected ServiceUnavailable for contenders, got %v", err)
		}
	}
}

func TestBreaker_ClosedPanicCountsAsFailure(t *testing.T) {
	b := New(Config{
		MaxFailures:  1,
		ResetTimeout: time.Minute,
	})

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic to propagate")
			}
		}()
		_ = b.Call(func() error { panic("boom") })
	}()

	err := b.Call(func() error { return nil })
	if !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("expected breaker to open after panic, got %v", err)
	}
}

func TestBreaker_ClosedSuccessFromOlderCallDoesNotCloseOpenedCircuit(t *testing.T) {
	b := New(Config{
		MaxFailures:  1,
		ResetTimeout: time.Minute,
	})

	successStarted := make(chan struct{})
	releaseSuccess := make(chan struct{})
	successDone := make(chan error, 1)
	go func() {
		successDone <- b.Call(func() error {
			close(successStarted)
			<-releaseSuccess
			return nil
		})
	}()

	select {
	case <-successStarted:
	case <-time.After(time.Second):
		t.Fatal("success call did not start")
	}

	failErr := errors.New("boom")
	if err := b.Call(func() error { return failErr }); err != failErr {
		t.Fatalf("expected failing call error, got %v", err)
	}

	close(releaseSuccess)
	if err := <-successDone; err != nil {
		t.Fatalf("success call should return nil, got %v", err)
	}

	if err := b.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("expected circuit to remain open after older success, got %v", err)
	}
}

func TestBreaker_OlderClosedSuccessDoesNotCloseActiveHalfOpenProbe(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	b := New(Config{
		MaxFailures:  1,
		ResetTimeout: time.Minute,
		Clock:        clk,
	})

	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	oldDone := make(chan error, 1)
	go func() {
		oldDone <- b.Call(func() error {
			close(oldStarted)
			<-releaseOld
			return nil
		})
	}()

	select {
	case <-oldStarted:
	case <-time.After(time.Second):
		t.Fatal("old closed call did not start")
	}

	failErr := errors.New("boom")
	if err := b.Call(func() error { return failErr }); err != failErr {
		t.Fatalf("expected failing call error, got %v", err)
	}

	clk.Advance(time.Minute + time.Second)

	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	probeDone := make(chan error, 1)
	go func() {
		probeDone <- b.Call(func() error {
			close(probeStarted)
			<-releaseProbe
			return nil
		})
	}()

	select {
	case <-probeStarted:
	case <-time.After(time.Second):
		t.Fatal("half-open probe did not start")
	}

	close(releaseOld)
	if err := <-oldDone; err != nil {
		t.Fatalf("old closed call should return nil, got %v", err)
	}

	if err := b.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("expected active half-open probe to keep rejecting new calls, got %v", err)
	}

	close(releaseProbe)
	if err := <-probeDone; err != nil {
		t.Fatalf("half-open probe should succeed, got %v", err)
	}
	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("expected circuit closed after probe success, got %v", err)
	}
}
