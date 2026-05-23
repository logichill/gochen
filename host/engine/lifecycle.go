// Package app 定义了应用服务的生命周期管理接口和运行时契约。
package engine

import (
	"context"
	"time"

	"gochen/logging"
)

// State 定义服务器生命周期状态。
type State int

const (
	// StatePending 等待初始化
	StatePending State = iota
	// StateInitializing 正在初始化配置和基础环境
	StateInitializing
	// StatePrepared 资源已就绪（依赖检查通过），等待启动
	StatePrepared
	// StateRunning 服务正在运行
	StateRunning
	// StateStopping 正在执行优雅关闭
	StateStopping
	// StateStopped 服务已停止
	StateStopped
	// StateError 发生不可恢复的错误
	StateError
)

// String 返回生命周期状态的稳定文本标签，供日志和诊断使用。
func (s State) String() string {
	switch s {
	case StatePending:
		return "Pending"
	case StateInitializing:
		return "Initializing"
	case StatePrepared:
		return "Prepared"
	case StateRunning:
		return "Running"
	case StateStopping:
		return "Stopping"
	case StateStopped:
		return "Stopped"
	case StateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// Hook 定义生命周期回调函数。
// ctx 可用于超时控制。
type Hook func(ctx context.Context) error

// Options 服务器启动配置选项。
type Options struct {
	Name    string
	Version string
	// ID 服务实例唯一标识符（预留字段）
	//
	// 当前未使用，预留用于以下场景：
	// - 服务注册与发现：在注册中心标识服务实例
	// - 分布式追踪：关联日志和追踪信息
	// - 集群管理：区分同一服务的不同实例
	//
	// 如果未设置，可由服务器启动时自动生成（如使用 UUID 或 Snowflake ID）。
	ID              string
	Metadata        map[string]string
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	Logger          logging.ILogger

	// 生命周期回调
	//
	// 语义约定（强约束，避免使用者误判）：
	// - fail-fast：OnBeforeInit / OnAfterInit / OnBeforeStart（任一 hook 返回 error 会中断启动并返回错误）
	// - warn-only：OnAfterStart / OnBeforeStop / OnAfterStop（任一 hook 返回 error 仅记录日志，不影响主流程）
	OnBeforeInit  []Hook
	OnAfterInit   []Hook
	OnBeforeStart []Hook
	OnAfterStart  []Hook
	OnBeforeStop  []Hook
	OnAfterStop   []Hook
}

// Option 配置修改函数。
type Option func(*Options)

// DefaultOptions 返回一组可直接用于 Engine 的默认启动配置。
func DefaultOptions() *Options {
	return &Options{
		Name:            "gochen-app",
		Version:         "0.0.0",
		StartupTimeout:  30 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		Metadata:        make(map[string]string),
		Logger:          logging.ComponentLogger("app.engine"),
	}
}

// WithName 覆盖服务名称。
func WithName(name string) Option {
	return func(o *Options) {
		o.Name = name
	}
}

// WithVersion 覆盖服务版本号。
func WithVersion(version string) Option {
	return func(o *Options) {
		o.Version = version
	}
}

// WithStartupTimeout 设置启动阶段的超时时间。
//
// 该超时会用于约束依赖初始化与后台任务启动探测阶段，避免启动流程无限阻塞。
func WithStartupTimeout(t time.Duration) Option {
	return func(o *Options) {
		o.StartupTimeout = t
	}
}

// WithShutdownTimeout 设置优雅关闭阶段的超时时间。
func WithShutdownTimeout(t time.Duration) Option {
	return func(o *Options) {
		o.ShutdownTimeout = t
	}
}

// WithLogger 替换 Engine 使用的日志实现。
func WithLogger(logger logging.ILogger) Option {
	return func(o *Options) {
		if logger != nil {
			o.Logger = logger
		}
	}
}

// WithBeforeInit 添加初始化前回调（fail-fast）。
func WithBeforeInit(fn Hook) Option {
	return func(o *Options) {
		if fn != nil {
			o.OnBeforeInit = append(o.OnBeforeInit, fn)
		}
	}
}

// WithAfterInit 添加初始化后回调（fail-fast）。
func WithAfterInit(fn Hook) Option {
	return func(o *Options) {
		if fn != nil {
			o.OnAfterInit = append(o.OnAfterInit, fn)
		}
	}
}

// WithBeforeStart 添加启动前回调（fail-fast）。
func WithBeforeStart(fn Hook) Option {
	return func(o *Options) {
		if fn != nil {
			o.OnBeforeStart = append(o.OnBeforeStart, fn)
		}
	}
}

// WithAfterStart 添加启动后回调（warn-only）。
func WithAfterStart(fn Hook) Option {
	return func(o *Options) {
		if fn != nil {
			o.OnAfterStart = append(o.OnAfterStart, fn)
		}
	}
}

// WithBeforeStop 添加停止前回调（warn-only）。
func WithBeforeStop(fn Hook) Option {
	return func(o *Options) {
		if fn != nil {
			o.OnBeforeStop = append(o.OnBeforeStop, fn)
		}
	}
}

// WithAfterStop 添加停止后回调（warn-only）。
func WithAfterStop(fn Hook) Option {
	return func(o *Options) {
		if fn != nil {
			o.OnAfterStop = append(o.OnAfterStop, fn)
		}
	}
}
