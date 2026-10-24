package circuit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gochen/clock"
	"gochen/errors"
)

// 取消豁免矩阵（P1-4）：
//   - 调用方主动取消 → 不计失败；
//   - 熔断器自身超时（CallTimeout）→ 计失败并跳闸；
//   - 下游纯超时（handler 派生 deadline 到期返回 DeadlineExceeded）→ 计失败并跳闸。
//
// Call 没有 ctx 信息，无法区分取消来源，任何错误一律计失败。

// TestBreaker_CallContext_CallerCancelNotCountedAsFailure 调用方主动取消不计失败。
func TestBreaker_CallContext_CallerCancelNotCountedAsFailure(t *testing.T) {
	b := New(Config{MaxFailures: 1, ResetTimeout: time.Minute})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := b.CallContext(ctx, func(ctx context.Context) error {
		cancel() // 调用方在执行期间主动取消
		<-ctx.Done()
		return fmt.Errorf("db query aborted: %w", ctx.Err())
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected caller cancellation error to propagate, got %v", err)
	}

	// 未计失败：MaxFailures=1 下熔断器必须仍然闭合放行。
	if err := b.Call(func() error { return nil }); err != nil {
		t.Fatalf("caller cancellation must not count as failure, breaker should stay closed, got %v", err)
	}
}

// TestBreaker_CallContext_CallerCancelReleasesHalfOpenProbe 调用方取消豁免在
// HalfOpen 试探上仅释放 busy 标记，不判失败、不回 Open。
func TestBreaker_CallContext_CallerCancelReleasesHalfOpenProbe(t *testing.T) {
	clk := clock.NewManualClock(time.Unix(0, 0).UTC())
	b := New(Config{MaxFailures: 1, ResetTimeout: time.Minute, Clock: clk})

	// 1) 跳闸进入 Open。
	_ = b.Call(func() error { return errors.New("boom") })

	// 2) 冷却结束 → HalfOpen 试探被调用方取消。
	clk.Advance(time.Minute + time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	err := b.CallContext(ctx, func(ctx context.Context) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", err)
	}

	// 3) 试探被豁免：熔断器未回 Open，下一个请求可立即再次试探。
	probed := false
	if err := b.Call(func() error { probed = true; return nil }); err != nil {
		t.Fatalf("expected a new half-open probe after caller-canceled probe, got %v", err)
	}
	if !probed {
		t.Fatal("expected probe fn to execute")
	}
}

// TestBreaker_CallContext_BreakerTimeoutCountsAsFailureAndTrips 熔断器自身
// CallTimeout 到期 → 计失败并跳闸（下游在熔断器预算内未完成，是过慢信号）。
func TestBreaker_CallContext_BreakerTimeoutCountsAsFailureAndTrips(t *testing.T) {
	b := New(Config{
		MaxFailures:  1,
		ResetTimeout: time.Minute,
		CallTimeout:  20 * time.Millisecond,
	})

	err := b.CallContext(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return fmt.Errorf("downstream interrupted: %w", ctx.Err())
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected ctx cancellation error, got %v", err)
	}

	// 计失败并跳闸：MaxFailures=1 → Open。
	if err := b.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("breaker timeout must count as failure and trip the breaker, got %v", err)
	}
}

// TestBreaker_CallContext_DownstreamTimeoutCountsAsFailureAndTrips 下游纯超时
// （handler 派生 deadline 到期返回 DeadlineExceeded，调用方 ctx 存活）→ 计失败
// 并跳闸。这是旧 isCallerCancellation 实现的核心误豁免场景。
func TestBreaker_CallContext_DownstreamTimeoutCountsAsFailureAndTrips(t *testing.T) {
	b := New(Config{MaxFailures: 1, ResetTimeout: time.Minute})

	err := b.CallContext(context.Background(), func(ctx context.Context) error {
		// 下游用自己的派生 deadline（模拟 handler 内对下游调用的超时控制），
		// 到期后以 DeadlineExceeded 上抛；调用方 ctx 全程存活。
		dctx, dcancel := context.WithTimeout(ctx, 5*time.Millisecond)
		defer dcancel()
		<-dctx.Done()
		return fmt.Errorf("db query: %w", dctx.Err())
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected downstream timeout error, got %v", err)
	}

	if err := b.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("downstream timeout must count as failure and trip the breaker, got %v", err)
	}
}

// TestBreaker_Call_CountsContextErrorsAsFailure Call 无 ctx 信息，ctx 形态错误
// 一律计失败（对齐 HEAD 语义，熔断保护不被削弱）。
func TestBreaker_Call_CountsContextErrorsAsFailure(t *testing.T) {
	b := New(Config{MaxFailures: 1, ResetTimeout: time.Minute})

	_ = b.Call(func() error { return fmt.Errorf("client gone: %w", context.Canceled) })

	if err := b.Call(func() error { return nil }); !errors.Is(err, errors.ServiceUnavailable) {
		t.Fatalf("Call must count ctx-shaped errors as failures, got %v", err)
	}
}

// TestBreaker_CallContext_NilFnRejected CallContext 与 Call 一样拒绝 nil fn。
func TestBreaker_CallContext_NilFnRejected(t *testing.T) {
	b := New(Config{})
	err := b.CallContext(context.Background(), nil)
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput for nil fn, got %v", err)
	}
}
