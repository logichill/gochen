package nethttp

import (
	"context"
	"gochen/errors"
	"sync"
	"testing"
	"time"
)

type mockManagedServer struct {
	name      string
	startFunc func(ctx context.Context) error
	stopFunc  func(ctx context.Context) error
}

// Start 启动数据。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s mockManagedServer) Start(ctx context.Context) error { return s.startFunc(ctx) }

// Stop 停止后台服务并释放资源。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s mockManagedServer) Stop(ctx context.Context) error { return s.stopFunc(ctx) }

// Name 返回名称。
//
// 返回：
// - result：文本结果
func (s mockManagedServer) Name() string { return s.name }

// TestManager_Run_StartError_ClosesAllServersInReverseOrder 验证 Manager Run StartError ClosesAllServersInReverseOrder。
func TestManager_Run_StartError_ClosesAllServersInReverseOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu      sync.Mutex
		stopped []string
	)
	recordStop := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		stopped = append(stopped, name)
	}

	s1 := mockManagedServer{
		name: "s1",
		startFunc: func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		},
		stopFunc: func(context.Context) error {
			recordStop("s1")
			return nil
		},
	}
	s2 := mockManagedServer{
		name: "s2",
		startFunc: func(ctx context.Context) error {
			return errors.New("start failed")
		},
		stopFunc: func(context.Context) error {
			recordStop("s2")
			return nil
		},
	}

	m := NewManager().WithShutdownTimeout(200*time.Millisecond).WithServers(s1, s2)
	err := m.Run(ctx)
	if err == nil {
		t.Fatalf("expected error")
	}

	mu.Lock()
	got := append([]string(nil), stopped...)
	mu.Unlock()

	if len(got) != 2 || got[0] != "s2" || got[1] != "s1" {
		t.Fatalf("expected stop order [s2 s1], got %#v", got)
	}
}

// TestManager_Run_ContextCancelled_ReturnsNil 验证 Manager Run ContextCancelled ReturnsNil。
func TestManager_Run_ContextCancelled_ReturnsNil(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	stopped := make(chan struct{})

	s := mockManagedServer{
		name: "s",
		startFunc: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return nil
		},
		stopFunc: func(context.Context) error {
			close(stopped)
			return nil
		},
	}

	go func() {
		<-started
		cancel()
	}()

	m := NewManager().WithShutdownTimeout(200 * time.Millisecond).WithServers(s)
	if err := m.Run(parent); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected server stopped")
	}
}

// TestManager_Run_CloseBlocked_ReturnsAfterShutdownTimeout 验证阻塞 Close 受 ShutdownTimeout 约束。
func TestManager_Run_CloseBlocked_ReturnsAfterShutdownTimeout(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	closeEntered := make(chan struct{})

	s := mockManagedServer{
		name: "s",
		startFunc: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return nil
		},
		stopFunc: func(context.Context) error {
			close(closeEntered)
			select {}
		},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- NewManager().WithShutdownTimeout(30 * time.Millisecond).WithServers(s).Run(parent)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("server did not start")
	}
	cancel()

	select {
	case <-closeEntered:
	case <-time.After(time.Second):
		t.Fatalf("server stop was not called")
	}

	select {
	case err := <-runDone:
		if err == nil {
			t.Fatalf("expected timeout error")
		}
		if !errors.Is(err, errors.Timeout) {
			t.Fatalf("expected timeout error, got: %#v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Run did not return after ShutdownTimeout")
	}
}
