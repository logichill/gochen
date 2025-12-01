package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// lifecycleFakeServer 用于验证 Engine.Start 的正常生命周期调用顺序。
type lifecycleFakeServer struct {
	mu    sync.Mutex
	steps []string

	loadConfigErr         error
	setupDependenciesErr  error
	startBackgroundErr    error
	runErr                error
	shutdownErr           error
}

func (s *lifecycleFakeServer) record(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

func (s *lifecycleFakeServer) Name() string {
	return "lifecycle-fake"
}

func (s *lifecycleFakeServer) LoadConfig() error {
	s.record("LoadConfig")
	return s.loadConfigErr
}

func (s *lifecycleFakeServer) SetupDependencies(ctx context.Context) error {
	s.record("SetupDependencies")
	return s.setupDependenciesErr
}

func (s *lifecycleFakeServer) StartBackgroundTasks(ctx context.Context) error {
	s.record("StartBackgroundTasks")
	return s.startBackgroundErr
}

func (s *lifecycleFakeServer) Run(ctx context.Context) error {
	s.record("Run")
	return s.runErr
}

func (s *lifecycleFakeServer) Shutdown(ctx context.Context) error {
	s.record("Shutdown")
	return s.shutdownErr
}

func (s *lifecycleFakeServer) snapshotSteps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.steps))
	copy(out, s.steps)
	return out
}

// backgroundFakeServer 在 StartBackgroundTasks 中启动依赖 ctx 的后台 goroutine，
// 用于验证 Engine 在退出时会正确取消上下文，从而避免 goroutine 泄露。
type backgroundFakeServer struct {
	bgDone chan struct{}
}

func (s *backgroundFakeServer) Name() string {
	return "background-fake"
}

func (s *backgroundFakeServer) LoadConfig() error {
	return nil
}

func (s *backgroundFakeServer) SetupDependencies(ctx context.Context) error {
	return nil
}

func (s *backgroundFakeServer) StartBackgroundTasks(ctx context.Context) error {
	if s.bgDone == nil {
		s.bgDone = make(chan struct{})
	}
	go func() {
		<-ctx.Done()
		close(s.bgDone)
	}()
	return nil
}

func (s *backgroundFakeServer) Run(ctx context.Context) error {
	// 模拟一个很快就退出的主循环，触发 Engine 的 runErr 分支。
	return nil
}

func (s *backgroundFakeServer) Shutdown(ctx context.Context) error {
	return nil
}

// loadConfigErrorServer 用于验证 LoadConfig 失败时的错误传播与状态机行为。
type loadConfigErrorServer struct {
	err           error
	setupCalled   bool
	backgroundCalled bool
	runCalled     bool
	shutdownCalled bool
}

func (s *loadConfigErrorServer) Name() string {
	return "loadconfig-error-fake"
}

func (s *loadConfigErrorServer) LoadConfig() error {
	if s.err == nil {
		return errors.New("missing error in loadConfigErrorServer")
	}
	return s.err
}

func (s *loadConfigErrorServer) SetupDependencies(ctx context.Context) error {
	s.setupCalled = true
	return nil
}

func (s *loadConfigErrorServer) StartBackgroundTasks(ctx context.Context) error {
	s.backgroundCalled = true
	return nil
}

func (s *loadConfigErrorServer) Run(ctx context.Context) error {
	s.runCalled = true
	return nil
}

func (s *loadConfigErrorServer) Shutdown(ctx context.Context) error {
	s.shutdownCalled = true
	return nil
}

func TestEngineStart_LifecycleSuccess(t *testing.T) {
	server := &lifecycleFakeServer{}
	engine := NewEngine(server,
		WithShutdownTimeout(50*time.Millisecond),
	)

	if err := engine.Start(); err != nil {
		t.Fatalf("Engine.Start() returned error in success case: %v", err)
	}

	if engine.State() != StateStopped {
		t.Fatalf("expected engine state %v, got %v", StateStopped, engine.State())
	}

	steps := server.snapshotSteps()
	expected := []string{
		"LoadConfig",
		"SetupDependencies",
		"StartBackgroundTasks",
		"Run",
		"Shutdown",
	}

	if len(steps) != len(expected) {
		t.Fatalf("unexpected lifecycle steps length, expected %d, got %d; steps=%v", len(expected), len(steps), steps)
	}

	for i, step := range expected {
		if steps[i] != step {
			t.Fatalf("unexpected lifecycle step at index %d: expected %q, got %q (all steps=%v)", i, step, steps[i], steps)
		}
	}
}

func TestEngineStart_RunErrorPropagatesAndSetsErrorState(t *testing.T) {
	runErr := errors.New("run failed")
	server := &lifecycleFakeServer{
		runErr: runErr,
	}
	engine := NewEngine(server)

	err := engine.Start()
	if err == nil {
		t.Fatalf("expected error from Engine.Start() when Run fails, got nil")
	}
	if !errors.Is(err, runErr) {
		t.Fatalf("expected Engine.Start() error to wrap runErr, got: %v", err)
	}
	if !strings.Contains(err.Error(), "server execution error") {
		t.Fatalf("expected Engine.Start() error message to contain wrapper prefix, got: %v", err)
	}

	if engine.State() != StateError {
		t.Fatalf("expected engine state %v when Run fails, got %v", StateError, engine.State())
	}

	steps := server.snapshotSteps()
	expectedPrefix := []string{
		"LoadConfig",
		"SetupDependencies",
		"StartBackgroundTasks",
		"Run",
		"Shutdown",
	}

	if len(steps) != len(expectedPrefix) {
		t.Fatalf("unexpected lifecycle steps length in error case, expected %d, got %d; steps=%v", len(expectedPrefix), len(steps), steps)
	}
	for i, step := range expectedPrefix {
		if steps[i] != step {
			t.Fatalf("unexpected lifecycle step at index %d in error case: expected %q, got %q (all steps=%v)", i, step, steps[i], steps)
		}
	}
}

func TestEngineStart_LoadConfigErrorStopsEarly(t *testing.T) {
	sentinelErr := errors.New("config failed")
	server := &loadConfigErrorServer{err: sentinelErr}
	engine := NewEngine(server)

	err := engine.Start()
	if err == nil {
		t.Fatalf("expected error from Engine.Start() when LoadConfig fails, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("expected Engine.Start() error to wrap LoadConfig error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("expected Engine.Start() error message to contain load config prefix, got: %v", err)
	}

	if engine.State() != StateError {
		t.Fatalf("expected engine state %v when LoadConfig fails, got %v", StateError, engine.State())
	}

	if server.setupCalled || server.backgroundCalled || server.runCalled || server.shutdownCalled {
		t.Fatalf("expected no further lifecycle methods to be called after LoadConfig error, got: setup=%v, background=%v, run=%v, shutdown=%v",
			server.setupCalled, server.backgroundCalled, server.runCalled, server.shutdownCalled)
	}
}

func TestEngineStart_CancelsBackgroundTasksOnShutdown(t *testing.T) {
	server := &backgroundFakeServer{
		bgDone: make(chan struct{}),
	}
	engine := NewEngine(server,
		WithShutdownTimeout(50*time.Millisecond),
	)

	startTime := time.Now()
	if err := engine.Start(); err != nil {
		t.Fatalf("Engine.Start() returned error in background-cancel case: %v", err)
	}

	if engine.State() != StateStopped {
		t.Fatalf("expected engine state %v after successful shutdown, got %v", StateStopped, engine.State())
	}

	select {
	case <-server.bgDone:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("background task was not cancelled within expected time; Start duration=%s", time.Since(startTime))
	}
}

// concurrentFakeServer 用于在 -race 下验证 Engine 与 IServer 实现之间的并发交互。
// 它在 Run 中阻塞一段时间，并在 Shutdown 中记录调用次数，配合 Engine 的信号分支/错误分支
// 验证生命周期钩子在多 goroutine 下不会引入数据竞态。
type concurrentFakeServer struct {
	mu          sync.Mutex
	runCalls    int
	shutdownCalls int
}

func (s *concurrentFakeServer) Name() string { return "concurrent-fake" }

func (s *concurrentFakeServer) LoadConfig() error { return nil }

func (s *concurrentFakeServer) SetupDependencies(ctx context.Context) error { return nil }

func (s *concurrentFakeServer) StartBackgroundTasks(ctx context.Context) error { return nil }

func (s *concurrentFakeServer) Run(ctx context.Context) error {
	s.mu.Lock()
	s.runCalls++
	s.mu.Unlock()

	// 模拟一个短暂运行的主循环，随后返回 nil 触发 Engine 的“正常退出”分支。
	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
	}
	return nil
}

func (s *concurrentFakeServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.shutdownCalls++
	s.mu.Unlock()
	return nil
}

func (s *concurrentFakeServer) snapshot() (runCalls, shutdownCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runCalls, s.shutdownCalls
}

// TestEngineStart_ConcurrentSafe 确认 Engine 与 IServer 实现的交互在 -race 下无数据竞态。
func TestEngineStart_ConcurrentSafe(t *testing.T) {
	server := &concurrentFakeServer{}
	engine := NewEngine(server,
		WithShutdownTimeout(100*time.Millisecond),
	)

	if err := engine.Start(); err != nil {
		t.Fatalf("Engine.Start() returned error in concurrent-safe case: %v", err)
	}

	if engine.State() != StateStopped {
		t.Fatalf("expected engine state %v, got %v", StateStopped, engine.State())
	}

	runCalls, shutdownCalls := server.snapshot()
	if runCalls != 1 || shutdownCalls != 1 {
		t.Fatalf("unexpected run/shutdown calls: run=%d, shutdown=%d", runCalls, shutdownCalls)
	}
}
