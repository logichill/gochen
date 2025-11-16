package basic

import (
	"context"
	"errors"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gochen/di"
	"gochen/logging"
)

// Server 通用服务生命周期接口（供 Manager 管理）
type Server interface {
	Start(ctx context.Context) error
	Close() error
	Name() string
}

// Options 定义运行选项
type Options struct {
	ShutdownTimeout time.Duration
}

// Manager 统一管理多个 Server 的生命周期（启动/关闭/优雅退出）
type Manager struct {
	Container *di.Container
	logger    logging.Logger
	servers   []Server
	opts      Options
}

// NewManager 创建 Server 管理器
func NewManager() *Manager {
	return &Manager{
		Container: di.New(),
		logger:    logging.GetLogger(),
		servers:   make([]Server, 0),
		opts:      Options{ShutdownTimeout: 10 * time.Second},
	}
}

// WithLogger 设置日志实现
func (m *Manager) WithLogger(l logging.Logger) *Manager {
	if l != nil {
		m.logger = l
		logging.SetLogger(l)
	}
	return m
}

// WithServers 批量注册 Server
func (m *Manager) WithServers(svcs ...Server) *Manager {
	m.servers = append(m.servers, svcs...)
	return m
}

// Register 注册单个 Server
func (m *Manager) Register(s Server) *Manager { return m.WithServers(s) }

// WithShutdownTimeout 配置优雅退出超时
func (m *Manager) WithShutdownTimeout(d time.Duration) *Manager {
	if d > 0 {
		m.opts.ShutdownTimeout = d
	}
	return m
}

// Run 启动所有 Server，监听系统信号并优雅退出
func (m *Manager) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
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
		m.logger.Info(context.Background(), "shutdown signal received")
	case err := <-errCh:
		runErr = err
		cancel()
	}

	// 关闭：后启动先关闭
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), m.opts.ShutdownTimeout)
	defer cancelShutdown()

	closeErrors := make([]error, 0)
	for i := len(m.servers) - 1; i >= 0; i-- {
		s := m.servers[i]
		t0 := time.Now()
		if err := s.Close(); err != nil {
			m.logger.Warn(shutdownCtx, "server close error", logging.String("name", s.Name()), logging.Error(err))
			closeErrors = append(closeErrors, err)
		} else {
			m.logger.Info(shutdownCtx, "server closed", logging.String("name", s.Name()), logging.Int64("ms", time.Since(t0).Milliseconds()))
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
