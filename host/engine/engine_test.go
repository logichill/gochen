package engine

import (
	"context"
	"gochen/errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// lifecycleFakeServer 用于验证 Engine.Start 的正常生命周期调用顺序。
type lifecycleFakeServer struct {
	mu    sync.Mutex
	steps []string

	loadConfigErr        error
	setupDependenciesErr error
	startBackgroundErr   error
	runErr               error
	shutdownErr          error
}

func (s *lifecycleFakeServer) record(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

// Name 返回名称。
//
// 返回：
// - result：文本结果
func (s *lifecycleFakeServer) Name() string {
	return "lifecycle-fake"
}

// LoadConfig 解析数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *lifecycleFakeServer) LoadConfig() error {
	s.record("LoadConfig")
	return s.loadConfigErr
}

// Prepare 设置当前值。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *lifecycleFakeServer) Prepare(ctx context.Context) error {
	s.record("Prepare")
	return s.setupDependenciesErr
}

// StartBackground 启动后台任务（不阻塞主服务）。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *lifecycleFakeServer) StartBackground(ctx context.Context) error {
	s.record("StartBackgroundTasks")
	return s.startBackgroundErr
}

// Run 启动主服务并阻塞运行。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *lifecycleFakeServer) Run(ctx context.Context) error {
	s.record("Run")
	return s.runErr
}

// Shutdown 优雅关闭并释放资源。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *lifecycleFakeServer) Shutdown(ctx context.Context) error {
	s.record("Shutdown")
	return s.shutdownErr
}

// snapshotSteps result：列表结果（元素类型：string）。
//
// 返回：
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

// Name 返回名称。
//
// 返回：
// - result：文本结果
func (s *backgroundFakeServer) Name() string {
	return "background-fake"
}

// LoadConfig 解析数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *backgroundFakeServer) LoadConfig() error {
	return nil
}

// Prepare 设置当前值。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *backgroundFakeServer) Prepare(ctx context.Context) error {
	return nil
}

// StartBackground 启动后台任务（不阻塞主服务）。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *backgroundFakeServer) StartBackground(ctx context.Context) error {
	if s.bgDone == nil {
		s.bgDone = make(chan struct{})
	}
	go func() {
		<-ctx.Done()
		close(s.bgDone)
	}()
	return nil
}

// Run 启动主服务并阻塞运行。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *backgroundFakeServer) Run(ctx context.Context) error {
	// 模拟一个很快就退出的主循环，触发 Engine 的 runErr 分支。
	return nil
}

// Shutdown 优雅关闭并释放资源。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *backgroundFakeServer) Shutdown(ctx context.Context) error {
	return nil
}

// loadConfigErrorServer 用于验证 LoadConfig 失败时的错误传播与状态机行为。
type loadConfigErrorServer struct {
	err              error
	setupCalled      bool
	backgroundCalled bool
	runCalled        bool
	shutdownCalled   bool
}

// Name 返回名称。
//
// 返回：
// - result：文本结果
func (s *loadConfigErrorServer) Name() string {
	return "loadconfig-error-fake"
}

// LoadConfig 解析数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *loadConfigErrorServer) LoadConfig() error {
	if s.err == nil {
		return errors.New("missing error in loadConfigErrorServer")
	}
	return s.err
}

// Prepare 设置当前值。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *loadConfigErrorServer) Prepare(ctx context.Context) error {
	s.setupCalled = true
	return nil
}

// StartBackground 启动后台任务（不阻塞主服务）。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *loadConfigErrorServer) StartBackground(ctx context.Context) error {
	s.backgroundCalled = true
	return nil
}

// Run 启动主服务并阻塞运行。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *loadConfigErrorServer) Run(ctx context.Context) error {
	s.runCalled = true
	return nil
}

// Shutdown 优雅关闭并释放资源。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *loadConfigErrorServer) Shutdown(ctx context.Context) error {
	s.shutdownCalled = true
	return nil
}

// TestEngineStart_LifecycleSuccess 验证 EngineStart LifecycleSuccess。
func TestEngineStart_LifecycleSuccess(t *testing.T) {
	app := &lifecycleFakeServer{}
	engine, err := NewEngine(app,
		WithShutdownTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Engine.Start() returned error in success case: %v", err)
	}

	if engine.State() != StateStopped {
		t.Fatalf("expected engine state %v, got %v", StateStopped, engine.State())
	}

	steps := app.snapshotSteps()
	expected := []string{
		"LoadConfig",
		"Prepare",
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

// TestEngineStart_RunErrorPropagatesAndSetsErrorState 验证 EngineStart RunErrorPropagatesAndSetsErrorState。
func TestEngineStart_RunErrorPropagatesAndSetsErrorState(t *testing.T) {
	runErr := errors.New("run failed")
	app := &lifecycleFakeServer{
		runErr: runErr,
	}
	engine, err := NewEngine(app)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	startErr := engine.Start(context.Background())
	if startErr == nil {
		t.Fatalf("expected error from Engine.Start() when Run fails, got nil")
	}
	if !errors.Is(startErr, runErr) {
		t.Fatalf("expected Engine.Start() error to wrap runErr, got: %v", startErr)
	}
	if !strings.Contains(startErr.Error(), "app execution error") {
		t.Fatalf("expected Engine.Start() error message to contain wrapper prefix, got: %v", startErr)
	}

	if engine.State() != StateError {
		t.Fatalf("expected engine state %v when Run fails, got %v", StateError, engine.State())
	}

	steps := app.snapshotSteps()
	expectedPrefix := []string{
		"LoadConfig",
		"Prepare",
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

// TestEngineStart_LoadConfigErrorStopsEarly 验证 EngineStart LoadConfigErrorStopsEarly。
func TestEngineStart_LoadConfigErrorStopsEarly(t *testing.T) {
	sentinelErr := errors.New("config failed")
	app := &loadConfigErrorServer{err: sentinelErr}
	engine, err := NewEngine(app)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	startErr := engine.Start(context.Background())
	if startErr == nil {
		t.Fatalf("expected error from Engine.Start() when LoadConfig fails, got nil")
	}
	if !errors.Is(startErr, sentinelErr) {
		t.Fatalf("expected Engine.Start() error to wrap LoadConfig error, got: %v", startErr)
	}
	if !strings.Contains(startErr.Error(), "failed to load config") {
		t.Fatalf("expected Engine.Start() error message to contain load config prefix, got: %v", startErr)
	}

	if engine.State() != StateError {
		t.Fatalf("expected engine state %v when LoadConfig fails, got %v", StateError, engine.State())
	}

	if app.setupCalled || app.backgroundCalled || app.runCalled || app.shutdownCalled {
		t.Fatalf("expected no further lifecycle methods to be called after LoadConfig error, got: setup=%v, background=%v, run=%v, shutdown=%v",
			app.setupCalled, app.backgroundCalled, app.runCalled, app.shutdownCalled)
	}
}

func TestEngineStart_SetupError_StillCallsShutdown(t *testing.T) {
	sentinelErr := errors.New("setup failed")
	app := &lifecycleFakeServer{
		setupDependenciesErr: sentinelErr,
	}
	engine, err := NewEngine(app)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	startErr := engine.Start(context.Background())
	if startErr == nil {
		t.Fatal("expected setup error, got nil")
	}
	if !errors.Is(startErr, sentinelErr) {
		t.Fatalf("expected setup error to be preserved, got %v", startErr)
	}
	steps := app.snapshotSteps()
	expected := []string{"LoadConfig", "Prepare", "Shutdown"}
	if len(steps) != len(expected) {
		t.Fatalf("unexpected steps: %v", steps)
	}
	for i, step := range expected {
		if steps[i] != step {
			t.Fatalf("unexpected step at %d: want %q got %q (steps=%v)", i, step, steps[i], steps)
		}
	}
	if engine.State() != StateError {
		t.Fatalf("expected engine state %v, got %v", StateError, engine.State())
	}
}

func TestEngineStart_StartBackgroundError_StillCallsShutdown(t *testing.T) {
	sentinelErr := errors.New("background failed")
	app := &lifecycleFakeServer{
		startBackgroundErr: sentinelErr,
	}
	engine, err := NewEngine(app)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	startErr := engine.Start(context.Background())
	if startErr == nil {
		t.Fatal("expected background error, got nil")
	}
	if !errors.Is(startErr, sentinelErr) {
		t.Fatalf("expected background error to be preserved, got %v", startErr)
	}
	steps := app.snapshotSteps()
	expected := []string{"LoadConfig", "Prepare", "StartBackgroundTasks", "Shutdown"}
	if len(steps) != len(expected) {
		t.Fatalf("unexpected steps: %v", steps)
	}
	for i, step := range expected {
		if steps[i] != step {
			t.Fatalf("unexpected step at %d: want %q got %q (steps=%v)", i, step, steps[i], steps)
		}
	}
	if engine.State() != StateError {
		t.Fatalf("expected engine state %v, got %v", StateError, engine.State())
	}
}

// TestEngineStart_CancelsBackgroundTasksOnShutdown 验证 EngineStart CancelsBackgroundTasksOnShutdown。
func TestEngineStart_CancelsBackgroundTasksOnShutdown(t *testing.T) {
	app := &backgroundFakeServer{
		bgDone: make(chan struct{}),
	}
	engine, err := NewEngine(app,
		WithShutdownTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	startTime := time.Now()
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Engine.Start() returned error in background-cancel case: %v", err)
	}

	if engine.State() != StateStopped {
		t.Fatalf("expected engine state %v after successful shutdown, got %v", StateStopped, engine.State())
	}

	select {
	case <-app.bgDone:
		// ok
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("background task was not cancelled within expected time; Start duration=%s", time.Since(startTime))
	}
}

// concurrentFakeServer 用于在 -race 下验证 Engine 与 App 实现之间的并发交互。
// 它在 Run 中阻塞一段时间，并在 Shutdown 中记录调用次数，配合 Engine 的信号分支/错误分支
// 验证生命周期钩子在多 goroutine 下不会引入数据竞态。
type concurrentFakeServer struct {
	mu            sync.Mutex
	runCalls      int
	shutdownCalls int
}

// Name 返回名称。
//
// 返回：
// - result：文本结果
func (s *concurrentFakeServer) Name() string { return "concurrent-fake" }

// LoadConfig 解析数据。
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *concurrentFakeServer) LoadConfig() error { return nil }

// Prepare 设置当前值。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *concurrentFakeServer) Prepare(ctx context.Context) error { return nil }

// StartBackground 启动后台任务（不阻塞主服务）。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *concurrentFakeServer) StartBackground(ctx context.Context) error { return nil }

// Run 启动主服务并阻塞运行。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
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

// Shutdown 优雅关闭并释放资源。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (s *concurrentFakeServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.shutdownCalls++
	s.mu.Unlock()
	return nil
}

// snapshot runCalls：数值结果。
//
// 返回：
// - shutdownCalls：数值结果
func (s *concurrentFakeServer) snapshot() (runCalls, shutdownCalls int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runCalls, s.shutdownCalls
}

// TestEngineStart_ConcurrentSafe 验证 EngineStart ConcurrentSafe。
func TestEngineStart_ConcurrentSafe(t *testing.T) {
	app := &concurrentFakeServer{}
	engine, err := NewEngine(app,
		WithShutdownTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Engine.Start() returned error in concurrent-safe case: %v", err)
	}

	if engine.State() != StateStopped {
		t.Fatalf("expected engine state %v, got %v", StateStopped, engine.State())
	}

	runCalls, shutdownCalls := app.snapshot()
	if runCalls != 1 || shutdownCalls != 1 {
		t.Fatalf("unexpected run/shutdown calls: run=%d, shutdown=%d", runCalls, shutdownCalls)
	}
}

func TestEngineStart_Hooks_FailFast_OnBeforeInit(t *testing.T) {
	srv := &lifecycleFakeServer{}
	engine, err := NewEngine(srv,
		WithBeforeInit(func(context.Context) error {
			srv.record("Hook.BeforeInit")
			return errors.New("boom")
		}),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	startErr := engine.Start(context.Background())
	if startErr == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(startErr.Error(), "OnBeforeInit hook failed") {
		t.Fatalf("expected error to contain hook wrapper, got: %v", startErr)
	}

	steps := srv.snapshotSteps()
	if len(steps) != 1 || steps[0] != "Hook.BeforeInit" {
		t.Fatalf("unexpected steps: %v", steps)
	}
}

func TestEngineStart_Hooks_WarnOnly_OnAfterStart(t *testing.T) {
	srv := &lifecycleFakeServer{}
	engine, err := NewEngine(srv,
		WithAfterStart(func(context.Context) error {
			srv.record("Hook.AfterStart")
			return errors.New("after start failed")
		}),
		WithShutdownTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Engine.Start() returned error: %v", err)
	}

	steps := srv.snapshotSteps()
	found := false
	for _, s := range steps {
		if s == "Hook.AfterStart" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Hook.AfterStart to be executed, steps=%v", steps)
	}
}

func TestEngineStart_Hooks_WarnOnly_OnBeforeStopAndAfterStop_Order(t *testing.T) {
	srv := &lifecycleFakeServer{}
	engine, err := NewEngine(srv,
		WithBeforeStop(func(context.Context) error {
			srv.record("Hook.BeforeStop")
			return errors.New("before stop failed")
		}),
		WithAfterStop(func(context.Context) error {
			srv.record("Hook.AfterStop")
			return errors.New("after stop failed")
		}),
		WithShutdownTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Engine.Start() returned error: %v", err)
	}

	steps := srv.snapshotSteps()
	indexOf := func(needle string) int {
		for i, s := range steps {
			if s == needle {
				return i
			}
		}
		return -1
	}

	beforeStop := indexOf("Hook.BeforeStop")
	shutdown := indexOf("Shutdown")
	afterStop := indexOf("Hook.AfterStop")
	if beforeStop < 0 || shutdown < 0 || afterStop < 0 {
		t.Fatalf("expected hooks and shutdown to be executed, steps=%v", steps)
	}
	if !(beforeStop < shutdown && shutdown < afterStop) {
		t.Fatalf("expected Hook.BeforeStop < Shutdown < Hook.AfterStop, steps=%v", steps)
	}
}

func TestEngineStart_Hooks_WarnOnly_OnStartupFailure_StopHooksStillRun(t *testing.T) {
	srv := &lifecycleFakeServer{
		setupDependenciesErr: errors.New("setup failed"),
	}
	engine, err := NewEngine(srv,
		WithBeforeStop(func(context.Context) error {
			srv.record("Hook.BeforeStop")
			return errors.New("before stop failed")
		}),
		WithAfterStop(func(context.Context) error {
			srv.record("Hook.AfterStop")
			return errors.New("after stop failed")
		}),
		WithShutdownTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	startErr := engine.Start(context.Background())
	if startErr == nil {
		t.Fatal("expected startup error, got nil")
	}

	steps := srv.snapshotSteps()
	indexOf := func(needle string) int {
		for i, s := range steps {
			if s == needle {
				return i
			}
		}
		return -1
	}

	beforeStop := indexOf("Hook.BeforeStop")
	shutdown := indexOf("Shutdown")
	afterStop := indexOf("Hook.AfterStop")
	if beforeStop < 0 || shutdown < 0 || afterStop < 0 {
		t.Fatalf("expected startup failure to still execute stop hooks and shutdown, steps=%v", steps)
	}
	if !(beforeStop < shutdown && shutdown < afterStop) {
		t.Fatalf("expected Hook.BeforeStop < Shutdown < Hook.AfterStop, steps=%v", steps)
	}
}

type blockingSetupServer struct {
	setupCalled    bool
	shutdownCalled bool
}

func (s *blockingSetupServer) Name() string { return "blocking-setup" }
func (s *blockingSetupServer) LoadConfig() error {
	return nil
}
func (s *blockingSetupServer) Prepare(ctx context.Context) error {
	s.setupCalled = true
	<-ctx.Done()
	return ctx.Err()
}
func (s *blockingSetupServer) StartBackground(context.Context) error { return nil }
func (s *blockingSetupServer) Run(context.Context) error             { return nil }
func (s *blockingSetupServer) Shutdown(context.Context) error {
	s.shutdownCalled = true
	return nil
}

func TestEngineStart_CancelDuringSetup_IsGraceful(t *testing.T) {
	srv := &blockingSetupServer{}
	engine, err := NewEngine(srv,
		WithStartupTimeout(2*time.Second),
		WithShutdownTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Start(ctx) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error on cancellation during setup, got: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Engine.Start did not return in time after cancellation")
	}

	if !srv.setupCalled {
		t.Fatalf("expected Prepare to be called")
	}
	if !srv.shutdownCalled {
		t.Fatalf("expected Shutdown to be called on cancellation")
	}
}
