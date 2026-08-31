package main

import (
	"context"
	stderrors "errors"
	"log"
	"sync/atomic"
	"time"

	"gochen/clock"
	"gochen/errors"
	"gochen/policy/circuit"
	"gochen/policy/ratelimit"
	"gochen/policy/retry"
	"gochen/process/task"
)

func main() {
	ctx := context.Background()
	runRetry(ctx)
	runRateLimit()
	runCircuitBreaker()
	runSupervisedTask(ctx)
}

func runRetry(ctx context.Context) {
	attempts := 0
	err := retry.DoWithInfo(ctx, func(_ context.Context, attempt int) error {
		attempts = attempt
		if attempt < 3 {
			return stderrors.New("temporary dependency failure")
		}
		return nil
	}, retry.Config{
		MaxAttempts:   3,
		InitialDelay:  time.Millisecond,
		BackoffFactor: 2,
		MaxDelay:      2 * time.Millisecond,
	})
	must(err)
	log.Printf("retry succeeded after %d attempts", attempts)
}

func runRateLimit() {
	limiter := ratelimit.New(ratelimit.Config{RequestsPerSecond: 1, BurstSize: 2})
	first := limiter.Allow("tenant-a")
	second := limiter.Allow("tenant-a")
	third := limiter.Allow("tenant-a")
	if !first || !second || third {
		log.Fatal("unexpected token-bucket result")
	}
	log.Println("rate limiter accepted burst=2 and rejected the third request")
}

func runCircuitBreaker() {
	manualClock := clock.NewManualClock(time.Unix(0, 0).UTC())
	breaker := circuit.New(circuit.Config{
		MaxFailures:  1,
		ResetTimeout: time.Second,
		Clock:        manualClock,
	})
	dependencyErr := stderrors.New("dependency unavailable")
	if err := breaker.Call(func() error { return dependencyErr }); !stderrors.Is(err, dependencyErr) {
		log.Fatalf("first circuit call: %v", err)
	}
	if err := breaker.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		log.Fatalf("open circuit did not reject call: %v", err)
	}
	manualClock.Advance(time.Second + time.Nanosecond)
	must(breaker.Call(func() error { return nil }))
	log.Println("circuit breaker recovered through a successful half-open probe")
}

func runSupervisedTask(ctx context.Context) {
	supervisor := task.NewTaskSupervisor("example")
	done := make(chan struct{})
	var attempts atomic.Int32
	must(supervisor.GoWithRetry(ctx, "refresh-cache", 2, time.Millisecond, func(context.Context) error {
		if attempts.Add(1) < 3 {
			return stderrors.New("cache is warming")
		}
		close(done)
		return nil
	}))

	select {
	case <-done:
	case <-time.After(time.Second):
		log.Fatal("supervised task did not finish")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	must(supervisor.Stop(stopCtx))
	log.Printf("supervised task succeeded after %d attempts", attempts.Load())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
