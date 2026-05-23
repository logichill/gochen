package commandflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gochen/errors"
	"gochen/policy/retry"
)

func TestRun_RejectsNilContextAndAttempt(t *testing.T) {
	if _, err := Run[int](nil, Plan[int]{Attempt: func(context.Context, int) (int, error) {
		return 0, nil
	}}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Run nil ctx error = %v, want InvalidInput", err)
	}

	if _, err := Run[int](context.Background(), Plan[int]{}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("Run nil attempt error = %v, want InvalidInput", err)
	}
}

func TestRun_RetryIfControlsAttempts(t *testing.T) {
	boom := errors.New("boom")
	attempts := 0

	result, err := Run[int](context.Background(), Plan[int]{
		RetryConfig: retry.Config{
			MaxAttempts: 3,
			RetryIf:     func(error) bool { return false },
		},
		Attempt: func(context.Context, int) (int, error) {
			attempts++
			return attempts, boom
		},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Run error = %v, want boom", err)
	}
	if attempts != 1 || result.Attempts != 1 || result.State != 1 {
		t.Fatalf("attempt result = attempts:%d result:%+v, want one attempt", attempts, result)
	}
}

func TestRun_BackoffReturnsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	boom := errors.New("boom")

	result, err := Run[int](ctx, Plan[int]{
		RetryConfig: retry.Config{
			MaxAttempts:  2,
			InitialDelay: time.Hour,
			MaxDelay:     time.Hour,
			RetryIf:      func(error) bool { return true },
		},
		Attempt: func(context.Context, int) (int, error) {
			cancel()
			return 7, boom
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if result.Attempts != 1 || result.State != 7 {
		t.Fatalf("result = %+v, want canceled after first attempt", result)
	}
}

func TestRun_HookOrder(t *testing.T) {
	boom := errors.New("boom")
	events := make([]string, 0, 6)

	result, err := Run[int](context.Background(), Plan[int]{
		RetryConfig: retry.Config{
			MaxAttempts:   2,
			InitialDelay:  0,
			BackoffFactor: 1,
			RetryIf:       func(error) bool { return true },
		},
		Attempt: func(_ context.Context, attempt int) (int, error) {
			events = append(events, fmt.Sprintf("attempt:%d", attempt))
			if attempt == 1 {
				return 10, boom
			}
			return 20, nil
		},
		AfterAttempt: func(_ context.Context, attempt int, state int, err error) {
			events = append(events, fmt.Sprintf("after:%d:%d:%v", attempt, state, err != nil))
		},
		OnRetry: func(_ context.Context, attempt int, state int, err error, delay time.Duration) {
			events = append(events, fmt.Sprintf("retry:%d:%d:%s:%v", attempt, state, delay, err != nil))
		},
		AfterFinalize: func(_ context.Context, attempts int, state int, err error) {
			events = append(events, fmt.Sprintf("finalize:%d:%d:%v", attempts, state, err != nil))
		},
		Trace: func(_ context.Context, attempts int, _ time.Duration, err error) {
			events = append(events, fmt.Sprintf("trace:%d:%v", attempts, err != nil))
		},
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Attempts != 2 || result.State != 20 {
		t.Fatalf("result = %+v, want second attempt success", result)
	}

	want := []string{
		"attempt:1",
		"after:1:10:true",
		"retry:1:10:0s:true",
		"attempt:2",
		"after:2:20:false",
		"finalize:2:20:false",
		"trace:2:false",
	}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}
