# gochen/server

`gochen/server` 包提供了一套标准化的应用服务启动引擎（Engine），旨在规范化 Go 应用的生命周期管理。

它采用了 **模板方法 (Template Method)** 设计模式，将通用的启动流程（配置加载、依赖注入、信号监听、优雅关闭）封装在框架层，而将具体的业务实现留给使用者。

## 核心组件

### 1. `IServer` (接口)
这是**你需要实现**的接口。它定义了你的应用"怎么跑起来"。

```go
type IServer interface {
	Name() string
	LoadConfig() error
	SetupDependencies(ctx context.Context) error
	StartBackgroundTasks(ctx context.Context) error
	Run(ctx context.Context) error // 阻塞运行
	Shutdown(ctx context.Context) error
}
```

### 2. `Engine` (执行器)
这是**框架提供**的组件。它负责编排调用你的 `IServer` 实现，并处理脏活累活：
- 超时控制 (Timeout)
- 信号监听 (OS Signals: SIGINT, SIGTERM)
- 错误处理 (Error Handling)
- 状态回调 (Hooks)

## 使用指南

### 第一步：定义你的 Server 结构体

在你的 `app` 或 `main` 包中，创建一个结构体并实现 `server.IServer` 接口。

```go
package main

import (
    "context"
    "net/http"
    "time"

    "gochen/server"
)

type MyAPIServer struct {
    srv *http.Server
    // 其他依赖，如 db, redis...
}

func (s *MyAPIServer) Name() string { return "my-api-service" }

func (s *MyAPIServer) LoadConfig() error {
    // 加载配置逻辑...
    return nil
}

func (s *MyAPIServer) SetupDependencies(ctx context.Context) error {
    // 连接 DB, 初始化 Service...
    return nil
}

func (s *MyAPIServer) StartBackgroundTasks(ctx context.Context) error {
    // 启动 Cron, MQ Consumer...
    return nil
}

func (s *MyAPIServer) Run(ctx context.Context) error {
    s.srv = &http.Server{Addr: ":8080"}
    // 阻塞运行
    return s.srv.ListenAndServe()
}

func (s *MyAPIServer) Shutdown(ctx context.Context) error {
    // 优雅关闭
    return s.srv.Shutdown(ctx)
}
```

### 第二步：在 main 函数中启动 Engine

```go
func main() {
    app := &MyAPIServer{}
    
    // 创建 Engine，可配置超时等选项
    engine := server.NewEngine(app, 
        server.WithVersion("1.0.0"),
        server.WithStartupTimeout(30*time.Second),
    )
    
    // 一键启动
    if err := engine.Start(); err != nil {
        panic(err)
    }
}
```

## 高级特性：Hooks (回调)

你可以在不修改 `MyAPIServer` 代码的情况下，注入通用的逻辑（如服务注册、日志埋点）。

```go
engine := server.NewEngine(app,
    server.WithBeforeStart(func(ctx context.Context) error {
        fmt.Println("正在注册到服务发现中心...")
        return nil
    }),
    server.WithAfterStop(func(ctx context.Context) error {
        fmt.Println("正在清理临时文件...")
        return nil
    }),
)
```

## 启动流程详解

调用 `engine.Start()` 后，流程如下：

1.  **StateInitializing**:
    - 执行 `OnBeforeInit` Hooks
    - 调用 `server.LoadConfig()`
    - 执行 `OnAfterInit` Hooks
2.  **StatePrepared**:
    - 调用 `server.SetupDependencies(ctx)` (带 StartupTimeout)
3.  **StateRunning**:
    - 执行 `OnBeforeStart` Hooks
    - 调用 `server.StartBackgroundTasks(ctx)`
    - 异步调用 `server.Run(ctx)`
    - 阻塞等待 OS Signal (Ctrl+C) 或 Run 错误
4.  **StateStopping**:
    - 收到信号，开始停止
    - 执行 `OnBeforeStop` Hooks
    - 调用 `server.Shutdown(ctx)` (带 ShutdownTimeout)
    - 执行 `OnAfterStop` Hooks
5.  **Exit**: 退出程序

---
**Design Philosophy**: Separation of concerns. Your code focuses on *what* to do; `server.Engine` focuses on *when* and *how* to manage the lifecycle safely.

### 配置选项概览（Options）

`Engine` 通过一组 `Option` 函数配置启动行为，底层结构体为：

```go
type Options struct {
	Name            string            // 服务名称，默认 "gochen-server"
	Version         string            // 版本号，默认 "0.0.0"
	ID              string            // 实例 ID，可选
	Metadata        map[string]string // 额外元数据（标签等）
	StartupTimeout  time.Duration     // 启动阶段超时（依赖装配）
	ShutdownTimeout time.Duration     // 关闭阶段超时

	// 生命周期回调
	OnBeforeInit  []Hook
	OnAfterInit   []Hook
	OnBeforeStart []Hook
	OnAfterStart  []Hook
	OnBeforeStop  []Hook
	OnAfterStop   []Hook
}
```

常用的 `Option` 包括：

- `server.WithName(string)`：覆盖服务名称（默认会使用 `IServer.Name()`）；
- `server.WithVersion(string)`：设置版本号；
- `server.WithStartupTimeout(time.Duration)`：配置启动超时；
- `server.WithShutdownTimeout(time.Duration)`：配置关闭超时；
- `server.WithBeforeStart(Hook)` / `server.WithAfterStop(Hook)`：在启动前 / 停止后注入回调逻辑。

## 最佳实践与使用约定

下面是基于实际项目（如使用 DI、事件总线、计划任务等复杂应用）总结的推荐用法，这些约定无需修改 Engine 抽象即可覆盖大部分场景。

### 1. 日志与配置：引导日志 + 配置化日志

典型的"鸡蛋问题"是：加载配置时需要日志，而日志配置又在 Config 里。推荐分两层：

1. 在 `main` 中创建一个**最小可用的引导日志器**（只输出到 stdout/stderr，不依赖配置）；
2. 在 `IServer.LoadConfig` 中加载配置；
3. 在 `IServer.SetupDependencies` 中根据配置重新初始化正式日志器，并替换全局 logger。

示例：

```go
func main() {
	// 1. 引导日志（不依赖 Config）
	bootstrapLogger := logging.NewStdLogger("my-app")
	logging.SetLogger(bootstrapLogger)

	// 2. 创建 Engine，LoadConfig/SetupDependencies 内部可以使用日志
	engine := server.NewEngine(NewMyServer(),
		server.WithVersion("1.0.0"),
		server.WithStartupTimeout(30*time.Second),
		server.WithShutdownTimeout(10*time.Second),
	)

	if err := engine.Start(); err != nil {
		bootstrapLogger.Error(context.Background(), "server start failed",
			logging.Error(err),
		)
		os.Exit(1)
	}
}
```

在你的 `IServer` 实现中：

- `LoadConfig` 负责：读取配置 + 基本验证；
- `SetupDependencies` 负责：根据配置初始化 DI、数据库、HTTP 服务器、事件总线、正式日志器等。

### 2. 信号处理：让 Engine 接管

当你使用 `engine.Start()` 时：

- 不要在外层 `main` 中再对同一进程做 `signal.Notify` 和自定义优雅关闭逻辑；
- 由 Engine 统一监听 `SIGINT` / `SIGTERM` 并调用 `IServer.Shutdown` 完成优雅关闭；
- `Shutdown(ctx)` 应该是幂等、安全的，可以多次调用而不会出错。

如果你的项目对信号处理有非常复杂的自定义需求，可以选择：

- 直接在 `main` 中手写启动/关闭流程，而不是使用 `Engine`；
- 或者先只参考 `Engine` 的设计（状态机与方法划分），在项目内部自行实现一套。

### 3. 健康检查与就绪探针

Kubernetes 等环境通常通过 HTTP 路由来做健康检查与就绪检查，推荐模式：

- 在 `SetupDependencies` 中完成健康检查所需依赖的初始化；
- 在 `Run` 中启动 HTTP 服务器，暴露 `/health`、`/ready` 等路由；
- 在 `StartBackgroundTasks` 成功完成后，将内部状态标记为 "Ready"；
- `/ready` 路由根据内部状态决定是否返回 200。

伪代码示例：

```go
type MyServer struct {
	httpServer *http.Server
	ready      atomic.Bool
}

func (s *MyServer) SetupDependencies(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	s.httpServer = &http.Server{Addr: ":8080", Handler: mux}
	return nil
}

func (s *MyServer) StartBackgroundTasks(ctx context.Context) error {
	// 启动 MQ、定时任务、投影等
	// ...
	s.ready.Store(true) // 所有后台任务启动成功后标记为就绪
	return nil
}

func (s *MyServer) Run(ctx context.Context) error {
	return s.httpServer.ListenAndServe()
}
```

### 4. 复杂应用的映射建议

对于类似业务复杂度较高的应用（有 DI 容器、事件总线、投影、计划任务、Outbox 等），推荐将现有启动逻辑按以下方式映射到 `IServer`：

- `LoadConfig`：加载配置（含事件存储、消息总线、数据库、Outbox 等配置），并做基本验证；
- `SetupDependencies`：构建 DI 容器，初始化数据库、事件总线、路由、HTTP 服务器、投影管理器等；
- `StartBackgroundTasks`：依次启动消息传输层、投影、计划任务调度器、Outbox 管理器等后台组件；
- `Run`：启动 HTTP 服务器，阻塞监听；如使用标准库，注意对 `http.ErrServerClosed` 做特殊处理；
- `Shutdown`：按"外到内"或"内到外"顺序关闭后台组件（计划任务、Outbox、消息传输层、数据库连接、HTTP 服务器等）。

通过保持 Engine 抽象不变，仅在文档中约定这些最佳实践，可以在不增加复杂度的前提下覆盖从简单服务到复杂业务系统的大部分启动场景。
