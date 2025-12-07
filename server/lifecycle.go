// Package server 定义了应用服务的生命周期管理接口和运行时契约
package server

import (
	"context"
	"time"
)

// State 定义服务器生命周期状态
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

// String 返回状态的字符串表示
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

// Hook 定义生命周期回调函数
// ctx 可用于超时控制
type Hook func(ctx context.Context) error

// Options 服务器启动配置选项
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

	// 生命周期回调
	OnBeforeInit  []Hook
	OnAfterInit   []Hook
	OnBeforeStart []Hook
	OnAfterStart  []Hook
	OnBeforeStop  []Hook
	OnAfterStop   []Hook
}

// Option 配置修改函数
type Option func(*Options)

// DefaultOptions 获取默认配置
func DefaultOptions() *Options {
	return &Options{
		Name:            "gochen-server",
		Version:         "0.0.0",
		StartupTimeout:  30 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		Metadata:        make(map[string]string),
	}
}

// WithName 设置服务名称
func WithName(name string) Option {
	return func(o *Options) {
		o.Name = name
	}
}

// WithVersion 设置服务版本
func WithVersion(version string) Option {
	return func(o *Options) {
		o.Version = version
	}
}

// WithStartupTimeout 设置启动超时时间
func WithStartupTimeout(t time.Duration) Option {
	return func(o *Options) {
		o.StartupTimeout = t
	}
}

// WithShutdownTimeout 设置关闭超时时间
func WithShutdownTimeout(t time.Duration) Option {
	return func(o *Options) {
		o.ShutdownTimeout = t
	}
}

// WithBeforeStart 添加启动前回调
func WithBeforeStart(fn Hook) Option {
	return func(o *Options) {
		o.OnBeforeStart = append(o.OnBeforeStart, fn)
	}
}

// WithAfterStop 添加停止后回调
func WithAfterStop(fn Hook) Option {
	return func(o *Options) {
		o.OnAfterStop = append(o.OnAfterStop, fn)
	}
}
