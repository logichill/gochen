package circuit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gochen/errors"
)

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
