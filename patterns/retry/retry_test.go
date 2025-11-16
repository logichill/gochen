package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo_Success(t *testing.T) {
	cfg := DefaultConfig()
	attempts := 0

	err := Do(context.Background(), func(ctx context.Context) error {
		attempts++
		return nil // 第一次就成功
	}, cfg)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("Expected 1 attempt, got %d", attempts)
	}
}

func TestDo_RetryAndSuccess(t *testing.T) {
	cfg := DefaultConfig()
	attempts := 0

	err := Do(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary error")
		}
		return nil // 第二次成功
	}, cfg)

	if err != nil {
		t.Fatalf("Expected success after retry, got error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("Expected 2 attempts, got %d", attempts)
	}
}

func TestDo_AllAttemptsFail(t *testing.T) {
	cfg := Config{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Millisecond,
		BackoffFactor: 2.0,
		MaxDelay:      1 * time.Second,
	}
	attempts := 0
	expectedErr := errors.New("persistent error")

	err := Do(context.Background(), func(ctx context.Context) error {
		attempts++
		return expectedErr
	}, cfg)

	if err != expectedErr {
		t.Fatalf("Expected error '%v', got '%v'", expectedErr, err)
	}
	if attempts != 3 {
		t.Fatalf("Expected 3 attempts, got %d", attempts)
	}
}

func TestDo_ContextCancellation(t *testing.T) {
	cfg := Config{
		MaxAttempts:   10,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: 2.0,
		MaxDelay:      1 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0

	// 在第一次失败后取消上下文
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, func(ctx context.Context) error {
		attempts++
		return errors.New("error")
	}, cfg)

	if err != context.Canceled {
		t.Fatalf("Expected context.Canceled, got %v", err)
	}
	if attempts > 2 {
		t.Fatalf("Expected few attempts due to cancellation, got %d", attempts)
	}
}

func TestDo_ExponentialBackoff(t *testing.T) {
	cfg := Config{
		MaxAttempts:   4,
		InitialDelay:  10 * time.Millisecond,
		BackoffFactor: 2.0,
		MaxDelay:      1 * time.Second,
	}

	attempts := 0
	attemptTimes := []time.Time{}

	err := Do(context.Background(), func(ctx context.Context) error {
		attempts++
		attemptTimes = append(attemptTimes, time.Now())
		return errors.New("error")
	}, cfg)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if attempts != 4 {
		t.Fatalf("Expected 4 attempts, got %d", attempts)
	}

	// 验证退避延迟：10ms, 20ms, 40ms（允许±5ms误差）
	expectedDelays := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}
	for i := 1; i < len(attemptTimes); i++ {
		actualDelay := attemptTimes[i].Sub(attemptTimes[i-1])
		expectedDelay := expectedDelays[i-1]
		tolerance := 15 * time.Millisecond

		if actualDelay < expectedDelay-tolerance || actualDelay > expectedDelay+tolerance {
			t.Errorf("Attempt %d: expected delay ~%v, got %v", i+1, expectedDelay, actualDelay)
		}
	}
}

func TestDo_MaxDelay(t *testing.T) {
	cfg := Config{
		MaxAttempts:   5,
		InitialDelay:  100 * time.Millisecond,
		BackoffFactor: 10.0,                  // 大倍数，会快速超过 MaxDelay
		MaxDelay:      50 * time.Millisecond, // 最大延迟限制
	}

	attempts := 0
	attemptTimes := []time.Time{}

	err := Do(context.Background(), func(ctx context.Context) error {
		attempts++
		attemptTimes = append(attemptTimes, time.Now())
		return errors.New("error")
	}, cfg)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	// 验证所有延迟都不超过 MaxDelay
	for i := 1; i < len(attemptTimes); i++ {
		actualDelay := attemptTimes[i].Sub(attemptTimes[i-1])
		// 第一次延迟应该是 100ms，但会被限制为 50ms
		if actualDelay > cfg.MaxDelay+20*time.Millisecond {
			t.Errorf("Attempt %d: delay %v exceeds MaxDelay %v", i+1, actualDelay, cfg.MaxDelay)
		}
	}
}

func TestDoWithInfo_Success(t *testing.T) {
	cfg := DefaultConfig()
	attempts := 0
	receivedAttempts := []int{}

	err := DoWithInfo(context.Background(), func(ctx context.Context, attempt int) error {
		attempts++
		receivedAttempts = append(receivedAttempts, attempt)
		if attempt < 2 {
			return errors.New("fail first time")
		}
		return nil // 第二次成功
	}, cfg)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("Expected 2 attempts, got %d", attempts)
	}
	if len(receivedAttempts) != 2 || receivedAttempts[0] != 1 || receivedAttempts[1] != 2 {
		t.Fatalf("Expected attempts [1, 2], got %v", receivedAttempts)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxAttempts != 2 {
		t.Errorf("Expected MaxAttempts=2, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 2*time.Millisecond {
		t.Errorf("Expected InitialDelay=2ms, got %v", cfg.InitialDelay)
	}
	if cfg.BackoffFactor != 2.0 {
		t.Errorf("Expected BackoffFactor=2.0, got %f", cfg.BackoffFactor)
	}
	if cfg.MaxDelay != 1*time.Second {
		t.Errorf("Expected MaxDelay=1s, got %v", cfg.MaxDelay)
	}
}

func TestPow(t *testing.T) {
	tests := []struct {
		base, exp, expected float64
	}{
		{2, 0, 1},
		{2, 1, 2},
		{2, 2, 4},
		{2, 3, 8},
		{3, 2, 9},
		{10, 3, 1000},
	}

	for _, tt := range tests {
		result := pow(tt.base, tt.exp)
		if result != tt.expected {
			t.Errorf("pow(%v, %v) = %v, expected %v", tt.base, tt.exp, result, tt.expected)
		}
	}
}

// 并发安全测试
func TestDo_Concurrent(t *testing.T) {
	cfg := DefaultConfig()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			attempts := 0
			_ = Do(context.Background(), func(ctx context.Context) error {
				attempts++
				if attempts < 2 {
					return errors.New("fail")
				}
				return nil
			}, cfg)
			done <- true
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < 10; i++ {
		<-done
	}
}
