package runtime

import (
	auth "gochen/auth"
	"gochen/di"
	deventsourced "gochen/domain/eventsourced"
	"gochen/host/internal/bootstrap"
)

// Host 承载一组模块的轻量服务实现。
type Host struct {
	config    *HostConfig
	ctors     []ModuleCtor
	modules   []IModule
	container di.IContainer

	runtime          *bootstrap.Runtime
	authzRegistry    *auth.Registry
	moduleRegistry   *ModuleRegistry
	metadataRegistry *deventsourced.MetadataRegistry

	moduleStops []ModuleStopFunc

	moduleIDs map[IModule]string
}

// NewHost 创建承载模块集合的轻量服务。
func NewHost(ctors []ModuleCtor, opts ...Option) *Host {
	cfg := DefaultHostConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	cfg = ensureHostConfig(cfg)

	return &Host{
		config:    cfg,
		ctors:     ctors,
		container: cfg.Container,
	}
}

func (s *Host) Name() string {
	if s.config != nil && s.config.Name != "" {
		return s.config.Name
	}
	return "gochen-module-server"
}

// LoadConfig 加载并规范化服务配置。
func (s *Host) LoadConfig() error {
	s.config = ensureHostConfig(s.config)
	s.container = s.config.Container
	s.runtime = nil
	return nil
}
