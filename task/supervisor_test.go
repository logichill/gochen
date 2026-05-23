package task

import (
	"context"
	"gochen/errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"gochen/clock"
	"gochen/logging"
)

func TestTaskSupervisor_Go(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	executed := int32(0)

	if err := gm.Go(ctx, "test-task", func(ctx context.Context) {
		atomic.AddInt32(&executed, 1)
	}); err != nil {
		t.Fatalf("Go returned error: %v", err)
	}

	_ = gm.Stop(context.Background())

	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("expected task to execute once, got %d", executed)
	}
}

func TestTaskSupervisor_Go_PanicRecovery(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	if err := gm.Go(ctx, "panic-task", func(ctx context.Context) {
		panic("test panic")
	}); err != nil {
		t.Fatalf("Go returned error: %v", err)
	}

	// 应该不会崩溃
	_ = gm.Stop(context.Background())
}

func TestTaskSupervisor_GoLoop(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	counter := int32(0)

	if err := gm.GoLoop(ctx, "loop-task", 10*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&counter, 1)
		return nil
	}); err != nil {
		t.Fatalf("GoLoop returned error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	_ = gm.Stop(context.Background())

	count := atomic.LoadInt32(&counter)
	if count < 2 {
		t.Errorf("expected at least 2 executions, got %d", count)
	}
}

func TestTaskSupervisor_GoLoop_StopSignal(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	counter := int32(0)

	if err := gm.GoLoop(ctx, "loop-task", 10*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&counter, 1)
		return nil
	}); err != nil {
		t.Fatalf("GoLoop returned error: %v", err)
	}

	time.Sleep(25 * time.Millisecond)
	_ = gm.Stop(context.Background())

	finalCount := atomic.LoadInt32(&counter)

	// 等待一段时间，确保停止后不再执行
	time.Sleep(30 * time.Millisecond)

	if atomic.LoadInt32(&counter) != finalCount {
		t.Error("goroutine continued executing after stop")
	}
}

func TestTaskSupervisor_GoLoop_ContextCancellation(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx, cancel := context.WithCancel(context.Background())

	counter := int32(0)

	if err := gm.GoLoop(ctx, "loop-task", 10*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&counter, 1)
		return nil
	}); err != nil {
		t.Fatalf("GoLoop returned error: %v", err)
	}

	time.Sleep(25 * time.Millisecond)
	cancel() // 取消 context

	time.Sleep(30 * time.Millisecond)
	_ = gm.Stop(context.Background())

	// 应该在 context 取消后停止
	if atomic.LoadInt32(&counter) > 5 {
		t.Error("goroutine did not stop on context cancellation")
	}
}

func TestTaskSupervisor_GoWithTimeout(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	executed := int32(0)

	if err := gm.GoWithTimeout(ctx, "timeout-task", 50*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&executed, 1)
		return nil
	}); err != nil {
		t.Fatalf("GoWithTimeout returned error: %v", err)
	}

	_ = gm.Stop(context.Background())

	if atomic.LoadInt32(&executed) != 1 {
		t.Error("task did not execute")
	}
}

func TestTaskSupervisor_GoWithTimeout_Timeout(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	if err := gm.GoWithTimeout(ctx, "slow-task", 10*time.Millisecond, func(ctx context.Context) error {
		time.Sleep(100 * time.Millisecond) // 超过超时时间
		return nil
	}); err != nil {
		t.Fatalf("GoWithTimeout returned error: %v", err)
	}

	_ = gm.Stop(context.Background())

	failure := waitFailure(t, gm, "slow-task")
	if !errors.Is(failure.Err, errors.Timeout) {
		t.Fatalf("expected timeout failure, got: %#v", failure.Err)
	}
	if !errors.Is(failure.Err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline cause, got: %#v", failure.Err)
	}
	var appErr *errors.AppError
	if !errors.As(failure.Err, &appErr) {
		t.Fatalf("expected app error, got: %#v", failure.Err)
	}
	if got := appErr.Details()["best_effort"]; got != true {
		t.Fatalf("expected best_effort detail, got: %#v", got)
	}
}

func TestTaskSupervisor_GoWithTimeout_BestEffortDoesNotStopInnerFunction(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	started := make(chan struct{})
	release := make(chan struct{})
	finished := int32(0)

	if err := gm.GoWithTimeout(ctx, "ignore-context", 10*time.Millisecond, func(ctx context.Context) error {
		close(started)
		<-release
		atomic.StoreInt32(&finished, 1)
		return nil
	}); err != nil {
		t.Fatalf("GoWithTimeout returned error: %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("task did not start")
	}

	failure := waitFailure(t, gm, "ignore-context")
	if !errors.Is(failure.Err, errors.Timeout) {
		t.Fatalf("expected timeout failure, got: %#v", failure.Err)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelStop()
	if err := gm.Stop(stopCtx); err != nil {
		t.Fatalf("expected supervisor stop to be bounded by timeout supervision, got: %v", err)
	}
	if atomic.LoadInt32(&finished) != 0 {
		t.Fatalf("expected inner function to still be running until it cooperates")
	}

	close(release)
	waitUntil(t, func() bool {
		return atomic.LoadInt32(&finished) == 1
	}, "inner function finished")
}

func TestTaskSupervisor_GoWithTimeout_FnPanicDoesNotHang(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	if err := gm.GoWithTimeout(ctx, "panic-fn", 200*time.Millisecond, func(ctx context.Context) error {
		panic("boom")
	}); err != nil {
		t.Fatalf("GoWithTimeout returned error: %v", err)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelStop()
	if err := gm.Stop(stopCtx); err != nil {
		t.Fatalf("expected stop to complete, got: %v", err)
	}

	failures := gm.Failures()
	if len(failures) == 0 {
		t.Fatalf("expected at least one failure")
	}
	last := failures[len(failures)-1]
	if last.Task != "panic-fn" {
		t.Fatalf("expected task panic-fn, got %q", last.Task)
	}
}

func TestTaskSupervisor_Failures_Capped(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	for i := 0; i < 500; i++ {
		_ = gm.Run(ctx, "err-task", func(ctx context.Context) error {
			return errors.New("x")
		})
	}

	failures := gm.Failures()
	if len(failures) == 0 {
		t.Fatalf("expected failures to be recorded")
	}
	if len(failures) > 100 {
		t.Fatalf("expected failures to be capped at 100, got %d", len(failures))
	}
}

func waitFailure(t *testing.T, gm *TaskSupervisor, taskName string) GoroutineFailure {
	t.Helper()
	var out GoroutineFailure
	waitUntil(t, func() bool {
		failures := gm.Failures()
		for _, failure := range failures {
			if failure.Task == taskName {
				out = failure
				return true
			}
		}
		return false
	}, "failure recorded for "+taskName)
	return out
}

func waitUntil(t *testing.T, ok func() bool, label string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func TestTaskSupervisor_GoWithRetry(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	gm := NewTaskSupervisorWithClock("test", logging.NewNoopLogger(), clk)
	ctx := context.Background()

	attempts := int32(0)
	attemptCh := make(chan int32)

	if err := gm.GoWithRetry(ctx, "retry-task", 3, 10*time.Millisecond, func(ctx context.Context) error {
		count := atomic.AddInt32(&attempts, 1)
		attemptCh <- count
		if count < 3 {
			return errors.New("temporary error")
		}
		return nil
	}); err != nil {
		t.Fatalf("GoWithRetry returned error: %v", err)
	}

	waitAttempt(t, attemptCh, 1)
	advanceAndWaitAttempt(t, clk, attemptCh, 2, 10*time.Millisecond)
	advanceAndWaitAttempt(t, clk, attemptCh, 3, 20*time.Millisecond)
	_ = gm.Stop(context.Background())

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestTaskSupervisor_GoWithRetry_MaxRetries(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	gm := NewTaskSupervisorWithClock("test", logging.NewNoopLogger(), clk)
	ctx := context.Background()

	attempts := int32(0)
	attemptCh := make(chan int32)

	if err := gm.GoWithRetry(ctx, "failing-task", 3, 10*time.Millisecond, func(ctx context.Context) error {
		count := atomic.AddInt32(&attempts, 1)
		attemptCh <- count
		return errors.New("permanent error")
	}); err != nil {
		t.Fatalf("GoWithRetry returned error: %v", err)
	}

	waitAttempt(t, attemptCh, 1)
	advanceAndWaitAttempt(t, clk, attemptCh, 2, 10*time.Millisecond)
	advanceAndWaitAttempt(t, clk, attemptCh, 3, 20*time.Millisecond)
	advanceAndWaitAttempt(t, clk, attemptCh, 4, 40*time.Millisecond)
	_ = gm.Stop(context.Background())

	if atomic.LoadInt32(&attempts) != 4 { // 初始尝试 + 3 次重试
		t.Errorf("expected 4 attempts, got %d", attempts)
	}
}

func waitAttempt(t *testing.T, ch <-chan int32, want int32) {
	t.Helper()
	timeout := 5 * time.Second
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("attempt = %d, want %d", got, want)
		}
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for attempt %d", want)
	}
}

func advanceAndWaitAttempt(t *testing.T, clk *clock.ManualClock, ch <-chan int32, want int32, advance time.Duration) {
	t.Helper()

	timeout := 5 * time.Second

	// Under -race, goroutine scheduling can be delayed enough that the retry timer
	// may be Reset() *after* we've already advanced the manual clock by the intended
	// delay. Keep advancing in small steps until the expected attempt arrives.
	start := time.Now()
	clk.Advance(advance)
	for {
		select {
		case got := <-ch:
			if got != want {
				t.Fatalf("attempt = %d, want %d", got, want)
			}
			return
		default:
			clk.Advance(1 * time.Millisecond)
			yield()
			if time.Since(start) > timeout {
				t.Fatalf("timed out waiting for attempt %d", want)
			}
		}
	}
}

func yield() {
	for i := 0; i < 16; i++ {
		runtime.Gosched()
	}
}

func TestTaskSupervisor_Stop(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	if err := gm.Go(ctx, "quick-task", func(ctx context.Context) {
		time.Sleep(10 * time.Millisecond)
	}); err != nil {
		t.Fatalf("Go returned error: %v", err)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelStop()
	err := gm.Stop(stopCtx)
	if err != nil {
		t.Errorf("unexpected timeout error: %v", err)
	}
}

func TestTaskSupervisor_Stop_ContextTimeout(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	if err := gm.Go(ctx, "slow-task", func(ctx context.Context) {
		time.Sleep(200 * time.Millisecond)
	}); err != nil {
		t.Fatalf("Go returned error: %v", err)
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelStop()
	err := gm.Stop(stopCtx)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestTaskSupervisor_Stop_RejectsNewTasks(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())

	if err := gm.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if err := gm.Go(context.Background(), "go-after-stop", func(ctx context.Context) {}); !errors.Is(err, errors.Conflict) {
		t.Fatalf("Go error = %v, want conflict", err)
	}
	if err := gm.GoLoop(context.Background(), "loop-after-stop", time.Millisecond, func(ctx context.Context) error {
		return nil
	}); !errors.Is(err, errors.Conflict) {
		t.Fatalf("GoLoop error = %v, want conflict", err)
	}
	if err := gm.GoWithTimeout(context.Background(), "timeout-after-stop", time.Second, func(ctx context.Context) error {
		return nil
	}); !errors.Is(err, errors.Conflict) {
		t.Fatalf("GoWithTimeout error = %v, want conflict", err)
	}
	if err := gm.GoWithRetry(context.Background(), "retry-after-stop", 1, time.Millisecond, func(ctx context.Context) error {
		return nil
	}); !errors.Is(err, errors.Conflict) {
		t.Fatalf("GoWithRetry error = %v, want conflict", err)
	}
}

func TestTaskSupervisor_GoLoop_RejectsNonPositiveInterval(t *testing.T) {
	t.Parallel()
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())

	for _, interval := range []time.Duration{0, -1, -time.Second} {
		err := gm.GoLoop(context.Background(), "loop-task", interval, func(ctx context.Context) error {
			return nil
		})
		if !errors.Is(err, errors.InvalidInput) {
			t.Errorf("GoLoop(%v) error = %v, want InvalidInput", interval, err)
		}
	}
}

func TestTaskSupervisor_GoWithRetry_ValidatesCtxBeforeFn(t *testing.T) {
	t.Parallel()
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())

	// nil ctx should be reported before fn == nil
	//nolint:staticcheck
	err := gm.GoWithRetry(nil, "task", 1, time.Millisecond, nil)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("GoWithRetry(nil ctx) error = %v, want InvalidInput", err)
	}

	// empty taskName should be reported before fn == nil
	err = gm.GoWithRetry(context.Background(), "  ", 1, time.Millisecond, nil)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("GoWithRetry(empty taskName) error = %v, want InvalidInput", err)
	}
}

func TestTaskSupervisor_Stop_CanCompleteAfterTimeout(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	started := make(chan struct{})
	release := make(chan struct{})

	if err := gm.Go(context.Background(), "slow-task", func(ctx context.Context) {
		close(started)
		<-release
	}); err != nil {
		t.Fatalf("Go returned error: %v", err)
	}
	<-started

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Millisecond)
	err := gm.Stop(stopCtx)
	cancelStop()
	if !errors.Is(err, errors.Timeout) {
		t.Fatalf("Stop error = %v, want timeout", err)
	}

	close(release)
	stopCtx, cancelStop = context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := gm.Stop(stopCtx); err != nil {
		t.Fatalf("second Stop returned error: %v", err)
	}
}

func TestTaskSupervisor_Stop_ConcurrentGoDoesNotRace(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	release := make(chan struct{})

	if err := gm.Go(context.Background(), "anchor", func(ctx context.Context) {
		<-release
	}); err != nil {
		t.Fatalf("Go returned error: %v", err)
	}

	stopDone := make(chan error, 1)
	go func() {
		stopDone <- gm.Stop(context.Background())
	}()

	waitUntil(t, func() bool {
		err := gm.Go(context.Background(), "after-stop", func(ctx context.Context) {})
		return errors.Is(err, errors.Conflict)
	}, "supervisor entered stopping state")

	for i := 0; i < 64; i++ {
		_ = gm.Go(context.Background(), "after-stop", func(ctx context.Context) {})
	}
	close(release)

	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Stop")
	}
}

func TestTaskSupervisor_StopWithinParentDeadlineIgnoresParentCancellation(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	if err := gm.Go(context.Background(), "quick-task", func(ctx context.Context) {
		time.Sleep(10 * time.Millisecond)
	}); err != nil {
		t.Fatalf("Go returned error: %v", err)
	}

	if err := gm.StopWithinParentDeadline(parent, 100*time.Millisecond); err != nil {
		t.Fatalf("StopWithinParentDeadline returned error: %v", err)
	}
}

func TestNewStopContextDeadlineBehavior(t *testing.T) {
	t.Run("uses fallback without parent deadline", func(t *testing.T) {
		ctx, cancel := NewStopContext(nil, 50*time.Millisecond)
		defer cancel()

		assertDeadlineRemaining(t, ctx, 5*time.Millisecond, 50*time.Millisecond)
	})

	t.Run("uses minimum window for non-positive fallback", func(t *testing.T) {
		ctx, cancel := NewStopContext(nil, 0)
		defer cancel()

		assertDeadlineRemaining(t, ctx, -5*time.Millisecond, 20*time.Millisecond)
	})

	t.Run("uses parent deadline instead of fallback", func(t *testing.T) {
		parent, cancelParent := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancelParent()

		ctx, cancel := NewStopContext(parent, 5*time.Second)
		defer cancel()

		assertDeadlineRemaining(t, ctx, 5*time.Millisecond, 40*time.Millisecond)
	})

	t.Run("uses minimum window for expired parent deadline", func(t *testing.T) {
		parent, cancelParent := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
		defer cancelParent()

		ctx, cancel := NewStopContext(parent, 5*time.Second)
		defer cancel()

		assertDeadlineRemaining(t, ctx, -5*time.Millisecond, 20*time.Millisecond)
	})

	t.Run("ignores parent cancellation and preserves values", func(t *testing.T) {
		type stopContextKey struct{}
		parent := context.WithValue(context.Background(), stopContextKey{}, "value")
		parent, cancelParent := context.WithCancel(parent)
		cancelParent()

		ctx, cancel := NewStopContext(parent, 50*time.Millisecond)
		defer cancel()

		select {
		case <-ctx.Done():
			t.Fatalf("stop context should not inherit parent cancellation: %v", ctx.Err())
		default:
		}
		if got := ctx.Value(stopContextKey{}); got != "value" {
			t.Fatalf("expected parent value to be preserved, got %v", got)
		}
	})
}

func assertDeadlineRemaining(t *testing.T, ctx context.Context, minRemaining, maxRemaining time.Duration) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected stop context to have deadline")
	}
	remaining := time.Until(deadline)
	if remaining < minRemaining || remaining > maxRemaining {
		t.Fatalf("deadline remaining = %v, want between %v and %v", remaining, minRemaining, maxRemaining)
	}
}

func TestTaskSupervisor_MultipleGoroutines(t *testing.T) {
	gm := NewTaskSupervisorWithLogger("test", logging.NewNoopLogger())
	ctx := context.Background()

	counter := int32(0)

	// 启动多个 goroutine
	for i := 0; i < 10; i++ {
		if err := gm.Go(ctx, "task", func(ctx context.Context) {
			atomic.AddInt32(&counter, 1)
			time.Sleep(10 * time.Millisecond)
		}); err != nil {
			t.Fatalf("Go returned error: %v", err)
		}
	}

	_ = gm.Stop(context.Background())

	if atomic.LoadInt32(&counter) != 10 {
		t.Errorf("expected 10 executions, got %d", counter)
	}
}
