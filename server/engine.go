// Package server 定义了应用服务的生命周期管理接口和运行时契约
package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// -----------------------------------------------------------------------------
// 1. Provider Interface (The Template Hooks)
// -----------------------------------------------------------------------------

// IServer 定义了业务应用必须实现的生命周期钩子。
// 这是一个"模板方法"接口，使用者只需要实现这些特定的步骤。
// 实现者通常是应用层（app包）的具体结构体。
type IServer interface {
	// Name 应用名称
	Name() string

	// LoadConfig 加载配置
	// 步骤 1: 解析配置文件、环境变量、命令行参数
	LoadConfig() error

	// SetupDependencies 初始化依赖
	// 步骤 2: 连接数据库、Redis，构建 Service，组装 Handler (DI)
	SetupDependencies(ctx context.Context) error

	// StartBackgroundTasks 启动后台任务
	// 步骤 3: 启动 CronJob, MQ Consumer 等非阻塞任务
	StartBackgroundTasks(ctx context.Context) error

	// Run 启动主服务
	// 步骤 4: 启动 HTTP/gRPC 服务器
	// 注意: 这个方法应当是阻塞的，或者返回一个 error (如果启动失败)
	// 在 Engine 中，我们将把它放入 goroutine 中运行
	Run(ctx context.Context) error

	// Shutdown 清理资源
	// 步骤 5: 关闭连接池、刷新日志、停止后台任务
	Shutdown(ctx context.Context) error
}

// -----------------------------------------------------------------------------
// 2. Engine (The Facade & Orchestrator)
// -----------------------------------------------------------------------------

// Engine 负责编排应用的启动流程。
// 它实现了标准的启动算法：Init -> Setup -> Background -> Server -> Signal -> Shutdown
type Engine struct {
	server  IServer
	options *Options
	state   State
}

// NewEngine 创建一个启动引擎
func NewEngine(server IServer, opts ...Option) *Engine {
	options := DefaultOptions()
	// 优先使用 server.Name()，除非 option 覆盖
	if name := server.Name(); name != "" {
		options.Name = name
	}

	for _, o := range opts {
		o(options)
	}

	return &Engine{
		server:  server,
		options: options,
		state:   StatePending,
	}
}

// State 获取当前引擎状态
func (e *Engine) State() State {
	return e.state
}

func (e *Engine) log(msg string, args ...interface{}) {
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	message := fmt.Sprintf(msg, args...)
	fmt.Printf("%s [INFO] %s [server] %s\n", timestamp, e.options.Name, message)
}

// Start 执行标准的启动模板流程
// Flow: LoadConfig -> Setup -> StartBackground -> Run -> Wait Signal -> Shutdown
func (e *Engine) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.log("Starting application version %s...", e.options.Version)

	// ---------------------------------------------------------
	// Phase 1: Initialization (Config)
	// ---------------------------------------------------------
	e.state = StateInitializing

	// Execute Hooks: OnBeforeInit
	for _, hook := range e.options.OnBeforeInit {
		if err := hook(ctx); err != nil {
			return fmt.Errorf("OnBeforeInit hook failed: %w", err)
		}
	}

	if err := e.server.LoadConfig(); err != nil {
		e.state = StateError
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Execute Hooks: OnAfterInit
	for _, hook := range e.options.OnAfterInit {
		if err := hook(ctx); err != nil {
			return fmt.Errorf("OnAfterInit hook failed: %w", err)
		}
	}

	// ---------------------------------------------------------
	// Phase 2: Preparation (Dependencies)
	// ---------------------------------------------------------
	// 给依赖初始化一个超时时间，防止卡死
	setupCtx, setupCancel := context.WithTimeout(ctx, e.options.StartupTimeout)
	defer setupCancel()

	if err := e.server.SetupDependencies(setupCtx); err != nil {
		e.state = StateError
		return fmt.Errorf("failed to setup dependencies: %w", err)
	}

	e.state = StatePrepared

	// ---------------------------------------------------------
	// Phase 3: Execution (Background & Server)
	// ---------------------------------------------------------
	// Execute Hooks: OnBeforeStart
	for _, hook := range e.options.OnBeforeStart {
		if err := hook(ctx); err != nil {
			return fmt.Errorf("OnBeforeStart hook failed: %w", err)
		}
	}

	// 3.1 Start Background Tasks
	if err := e.server.StartBackgroundTasks(ctx); err != nil {
		e.state = StateError
		return fmt.Errorf("failed to start background tasks: %w", err)
	}

	// 3.2 Run Server (Non-blocking here, orchestrated by signal)
	e.state = StateRunning
	errChan := make(chan error, 1)

	go func() {
		e.log("Server is running...")
		errChan <- e.server.Run(ctx)
	}()

	// Execute Hooks: OnAfterStart
	for _, hook := range e.options.OnAfterStart {
		// 这里使用一个新的 Context，或者复用 ctx，注意 Hook 不应长时间阻塞
		if err := hook(ctx); err != nil {
			e.log("Warning: OnAfterStart hook failed: %v", err)
		}
	}

	// ---------------------------------------------------------
	// Phase 4: Running & Waiting
	// ---------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	var runErr error
	select {
	case err := <-errChan:
		if err == nil {
			e.log("Server stopped gracefully. Shutting down...")
			cancel()
			break
		}
		e.log("Server stopped with error: %v. Shutting down...", err)
		cancel()
		runErr = err
	case sig := <-quit:
		e.log("Received signal: %v. Shutting down...", sig)
		cancel()
	}

	// ---------------------------------------------------------
	// Phase 5: Shutdown
	// ---------------------------------------------------------
	e.state = StateStopping

	// Execute Hooks: OnBeforeStop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), e.options.ShutdownTimeout)
	defer shutdownCancel()

	for _, hook := range e.options.OnBeforeStop {
		if err := hook(shutdownCtx); err != nil {
			e.log("Warning: OnBeforeStop hook failed: %v", err)
		}
	}

	if err := e.server.Shutdown(shutdownCtx); err != nil {
		e.state = StateError
		e.log("Shutdown error: %v", err)
		return err
	}

	// Execute Hooks: OnAfterStop
	for _, hook := range e.options.OnAfterStop {
		if err := hook(shutdownCtx); err != nil {
			e.log("Warning: OnAfterStop hook failed: %v", err)
		}
	}

	if runErr != nil {
		e.state = StateError
		return fmt.Errorf("server execution error: %w", runErr)
	}

	e.state = StateStopped
	e.log("Shutdown complete.")
	return nil
}
