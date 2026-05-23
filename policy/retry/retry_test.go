package retry

import (
	"context"
	"net"
	"testing"
	"time"

	"gochen/errors"
)

type retryableSentinel struct{ retryable bool }

// Error 返回错误字符串。
//
// 返回：
// - result：文本结果
func (e retryableSentinel) Error() string { return "sentinel" }

// IsRetryable 判断是否满足条件。
//
// 返回：
// - result：是否满足条件
func (e retryableSentinel) IsRetryable() bool { return e.retryable }

type netErr struct {
	timeout   bool
	temporary bool
}

// Error 返回错误字符串。
//
// 返回：
// - result：文本结果
func (e netErr) Error() string { return "net" }

// Timeout result：是否满足条件。
//
// 返回：
func (e netErr) Timeout() bool { return e.timeout }

// Temporary result：是否满足条件。
//
// 返回：
func (e netErr) Temporary() bool { return e.temporary }

// TestDo_Success 验证 Do Success。
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

// TestDo_NilContext 验证 Do NilContext。
func TestDo_NilContext(t *testing.T) {
	cfg := DefaultConfig()
	called := false

	err := Do(nil, func(ctx context.Context) error {
		called = true
		return nil
	}, cfg)

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if code := errors.Code(err); code != errors.InvalidInput {
		t.Fatalf("expected error code %s, got %s", errors.InvalidInput, code)
	}
	if called {
		t.Fatalf("operation should not be called when ctx is nil")
	}
}

// TestDo_NilOperation 验证 Do NilOperation。
func TestDo_NilOperation(t *testing.T) {
	cfg := DefaultConfig()

	err := Do(context.Background(), nil, cfg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if code := errors.Code(err); code != errors.InvalidInput {
		t.Fatalf("expected error code %s, got %s", errors.InvalidInput, code)
	}
}

// TestDo_RetryAndSuccess 验证 Do RetryAndSuccess。
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

// TestDo_AllAttemptsFail 验证 Do AllAttemptsFail。
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

// TestDo_ContextCancellation 验证 Do ContextCancellation。
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

// TestDo_ExponentialBackoff 验证 Do ExponentialBackoff。
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

// TestDo_MaxDelay 验证 Do MaxDelay。
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

// TestDoWithInfo_Success 验证 DoWithInfo Success。
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

// TestDefaultConfig 验证 DefaultConfig。
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

// TestPow 验证 Pow。
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

// TestIsRetryable 验证 IsRetryable。
func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatalf("nil should not be retryable")
	}
	if IsRetryable(context.Canceled) {
		t.Fatalf("context.Canceled should not be retryable")
	}
	if IsRetryable(context.DeadlineExceeded) {
		t.Fatalf("context.DeadlineExceeded should not be retryable")
	}

	var ne net.Error = netErr{timeout: true}
	if !IsRetryable(ne) {
		t.Fatalf("net timeout should be retryable")
	}
	if !IsRetryable(retryableSentinel{retryable: true}) {
		t.Fatalf("RetryableError(true) should be retryable")
	}
	if IsRetryable(retryableSentinel{retryable: false}) {
		t.Fatalf("RetryableError(false) should not be retryable")
	}
	if !IsRetryable(errors.New("other")) {
		t.Fatalf("default should be retryable for non-context errors")
	}
}

// TestDo_RetryIfStopsEarly 验证 Do RetryIfStopsEarly。
func TestDo_RetryIfStopsEarly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAttempts = 10
	cfg.InitialDelay = 0
	cfg.MaxDelay = 0
	cfg.RetryIf = func(err error) bool {
		var s retryableSentinel
		if errors.As(err, &s) && !s.retryable {
			return false
		}
		return true
	}

	attempts := 0
	err := Do(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts == 3 {
			return retryableSentinel{retryable: false}
		}
		return errors.New("retry")
	}, cfg)

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if attempts != 3 {
		t.Fatalf("expected to stop at attempt 3, got %d", attempts)
	}
}

// TestDo_DefaultRetryIfHonorsRetryableError 验证默认 RetryIf 遵循 IRetryableError 语义。
func TestDo_DefaultRetryIfHonorsRetryableError(t *testing.T) {
	cfg := Config{
		MaxAttempts: 3,
	}
	attempts := 0

	err := Do(context.Background(), func(ctx context.Context) error {
		_ = ctx
		attempts++
		return retryableSentinel{retryable: false}
	}, cfg)

	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("expected default RetryIf to stop after 1 attempt, got %d", attempts)
	}
}

// TestDo_ContextCanceledBeforeAttempt 验证取消的 context 不会执行 operation。
func TestDo_ContextCanceledBeforeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	attempts := 0
	err := DoWithInfo(ctx, func(ctx context.Context, attempt int) error {
		_ = ctx
		_ = attempt
		attempts++
		return nil
	}, Config{MaxAttempts: 3})

	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if attempts != 0 {
		t.Fatalf("expected no attempts after pre-cancel, got %d", attempts)
	}
}

// TestComputeDelay_NormalizesDefaults 验证退避计算与执行路径使用同一套归一化默认值。
func TestComputeDelay_NormalizesDefaults(t *testing.T) {
	cfg := Config{
		InitialDelay:  5 * time.Millisecond,
		BackoffFactor: 0,
		MaxDelay:      0,
	}

	if d := ComputeDelay(cfg, 3); d != 5*time.Millisecond {
		t.Fatalf("expected MaxDelay default to clamp at InitialDelay=5ms, got %v", d)
	}

	cfg.MaxDelay = 100 * time.Millisecond
	if d := ComputeDelay(cfg, 3); d != 20*time.Millisecond {
		t.Fatalf("expected BackoffFactor default 2.0 to yield 20ms, got %v", d)
	}
}

// TestDo_Concurrent 验证 Do Concurrent。
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

// TestComputeDelay_JitterClampsToMaxDelay 验证 jitter 不会突破 MaxDelay 上限（MaxDelay 为严格上界）。
func TestComputeDelay_JitterClampsToMaxDelay(t *testing.T) {
	old := jitterFloat64
	jitterFloat64 = func() float64 {
		// Force the max jitter path: jitter = +J.
		return 1
	}
	t.Cleanup(func() { jitterFloat64 = old })

	cfg := Config{
		InitialDelay:  100 * time.Millisecond,
		BackoffFactor: 10.0,
		MaxDelay:      50 * time.Millisecond,
		JitterRatio:   1,
	}

	d := ComputeDelay(cfg, 1)
	if d != cfg.MaxDelay {
		t.Fatalf("expected delay to be clamped to MaxDelay=%v, got %v", cfg.MaxDelay, d)
	}
}

// TestComputeDelay_JitterNeverNegative 验证 jitter 在异常随机数输入下也不会产生负延迟。
func TestComputeDelay_JitterNeverNegative(t *testing.T) {
	old := jitterFloat64
	jitterFloat64 = func() float64 {
		// Out of spec on purpose to ensure we clamp safely.
		return -1
	}
	t.Cleanup(func() { jitterFloat64 = old })

	cfg := Config{
		InitialDelay:  50 * time.Millisecond,
		BackoffFactor: 2.0,
		MaxDelay:      1 * time.Second,
		JitterRatio:   1,
	}

	d := ComputeDelay(cfg, 1)
	if d < 0 {
		t.Fatalf("expected non-negative delay, got %v", d)
	}
	if d != 0 {
		t.Fatalf("expected delay to clamp to 0, got %v", d)
	}
}
