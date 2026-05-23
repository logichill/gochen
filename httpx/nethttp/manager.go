package nethttp

import (
	"context"
	"sync"
	"time"

	"gochen/errors"
	"gochen/logging"
)

// IManagedServer 抽象Managed服务能力接口。
type IManagedServer interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Name() string
}

// Options 定义多服务管理器的运行选项。
type Options struct {
	ShutdownTimeout time.Duration
}

// Manager 负责统一启动和关闭一组受管 HTTP 服务。
type Manager struct {
	logger  logging.ILogger
	servers []IManagedServer
	opts    Options
}

// NewManager 创建带默认日志与关闭超时的管理器。
func NewManager() *Manager {
	return &Manager{
		logger:  logging.ComponentLogger("http.manager"),
		servers: make([]IManagedServer, 0),
		opts:    Options{ShutdownTimeout: 10 * time.Second},
	}
}

// WithLogger 设置管理器使用的日志实现。
func (m *Manager) WithLogger(l logging.ILogger) *Manager {
	if l != nil {
		m.logger = l
	}
	return m
}

// WithServers 追加一组受管服务。
func (m *Manager) WithServers(svcs ...IManagedServer) *Manager {
	m.servers = append(m.servers, svcs...)
	return m
}

// Register 追加一个受管服务。
func (m *Manager) Register(s IManagedServer) *Manager { return m.WithServers(s) }

// WithShutdownTimeout 配置统一优雅关闭超时。
func (m *Manager) WithShutdownTimeout(d time.Duration) *Manager {
	if d > 0 {
		m.opts.ShutdownTimeout = d
	}
	return m
}

// Run 启动全部受管服务，并在取消或任一服务失败时按逆序关闭。
func (m *Manager) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	m.logger.Info(ctx, "starting manager", logging.Int("servers", len(m.servers)))

	var wg sync.WaitGroup
	errCh := make(chan error, len(m.servers))

	startAt := time.Now()
	for _, s := range m.servers {
		srv := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			m.logger.Info(ctx, "server starting", logging.String("name", srv.Name()))
			if err := srv.Start(ctx); err != nil {
				m.logger.Error(ctx, "server start error", logging.String("name", srv.Name()), logging.Error(err))
				errCh <- err
				return
			}
			m.logger.Info(ctx, "server started", logging.String("name", srv.Name()), logging.Int64("ms", time.Since(t0).Milliseconds()))
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
		m.logger.Info(ctx, "shutdown requested")
	case err := <-errCh:
		runErr = err
		cancel()
	}

	// 关闭：后启动先关闭；保留 ctx values，但不继承取消信号（由 ShutdownTimeout 控制）。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(ctx), m.opts.ShutdownTimeout)
	defer cancelShutdown()

	closeErrors := make([]error, 0)
	for i := len(m.servers) - 1; i >= 0; i-- {
		s := m.servers[i]
		if err := shutdownCtx.Err(); err != nil {
			m.logger.Warn(shutdownCtx, "manager shutdown timeout", logging.Int64("timeout_ms", m.opts.ShutdownTimeout.Milliseconds()), logging.Error(err))
			closeErrors = append(closeErrors, errors.NewCodeWithCause(errors.Timeout, "manager shutdown timeout", err))
			break
		}
		t0 := time.Now()
		if err := m.stopServer(shutdownCtx, s); err != nil {
			m.logger.Warn(shutdownCtx, "server stop error", logging.String("name", s.Name()), logging.Error(err))
			closeErrors = append(closeErrors, err)
		} else {
			m.logger.Info(shutdownCtx, "server stopped", logging.String("name", s.Name()), logging.Int64("ms", time.Since(t0).Milliseconds()))
		}
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		m.logger.Info(shutdownCtx, "manager stopped", logging.Int64("ms", time.Since(startAt).Milliseconds()))
	case <-shutdownCtx.Done():
		m.logger.Warn(shutdownCtx, "manager shutdown timeout", logging.Int64("timeout_ms", m.opts.ShutdownTimeout.Milliseconds()))
	}

	if len(closeErrors) > 0 {
		runErr = errors.Join(append([]error{runErr}, closeErrors...)...)
	}
	return runErr
}

func (m *Manager) stopServer(ctx context.Context, s IManagedServer) error {
	done := make(chan error, 1)
	go func() {
		done <- s.Stop(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return errors.NewCodeWithCause(errors.Timeout, "server stop timeout", ctx.Err()).
			WithContext("server", s.Name()).
			WithContext("timeout", m.opts.ShutdownTimeout)
	}
}
