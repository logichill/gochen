package runtime

import (
	"context"

	"gochen/di"
	"gochen/errors"
	"gochen/host/internal/runtimeutil"
	"gochen/httpx"
)

// StartBackground 启动后台任务（不阻塞主服务）。
//
// 说明：
// - StartBackground 实现 IServer.StartBackground。
// - 该阶段包含“路由挂载”（RegisterRoutes）：只做 HTTP 路由装配与启动期校验，不进入运行期；
// - 再调用所有模块的 Start(ctx)（事件订阅/投影注册/后台任务等），失败则回滚；
// - 再启动消息传输层（Transport），避免遗漏订阅导致消息丢失。
func (s *Host) StartBackground(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := s.failFastCircularDependencies(); err != nil {
		return err
	}

	// 先挂载路由（若模块提供独立阶段），避免“借 Start 挂路由”引入运行期副作用。
	if len(s.modules) > 0 {
		for _, m := range s.modules {
			if m == nil {
				continue
			}
			rm, ok := m.(IRouteModule)
			if !ok {
				continue
			}
			if err := runtimeutil.NormalizeError(rm.RegisterRoutes(ctx)); err != nil {
				return errors.Wrap(err, errors.Dependency, "failed to register module routes").WithContext("module", m.Name())
			}
		}
	}

	if s.config != nil && s.config.FailFastOnRouteConflicts && s.runtime != nil && s.runtime.HTTPServer != nil {
		if rr, ok := s.runtime.HTTPServer.(httpx.IRouteRegistry); ok {
			if conflicts := rr.RouteConflicts(); len(conflicts) > 0 {
				return errors.NewCode(errors.InvalidInput, "route conflicts detected").WithContext("conflicts", conflicts)
			}
		}
	}

	if len(s.modules) > 0 {
		stops := make([]ModuleStopFunc, 0, len(s.modules))
		for _, m := range s.modules {
			if m == nil {
				continue
			}
			stop, err := m.Start(ctx)
			err = runtimeutil.NormalizeError(err)
			if err != nil {
				s.rollbackModuleStops(ctx, stops)
				return errors.Wrap(err, errors.Dependency, "failed to start module").WithContext("module", m.Name())
			}
			stops = append(stops, stop)
		}
		if s.runtime != nil && s.runtime.Transport != nil {
			if err := runtimeutil.NormalizeError(s.runtime.Transport.Start(ctx)); err != nil {
				s.rollbackModuleStops(ctx, stops)
				s.moduleStops = nil
				return errors.Wrap(err, errors.Dependency, "failed to start message transport")
			}
		}
		s.moduleStops = stops
		return nil
	}

	if s.runtime != nil && s.runtime.Transport != nil {
		if err := runtimeutil.NormalizeError(s.runtime.Transport.Start(ctx)); err != nil {
			return errors.Wrap(err, errors.Dependency, "failed to start message transport")
		}
	}
	return nil
}

func (s *Host) failFastCircularDependencies() error {
	if s == nil || s.container == nil {
		return nil
	}
	diag, ok := di.Diagnose(s.container)
	if !ok {
		return nil
	}
	if diag == nil || len(diag.CircularDependencies) == 0 {
		return nil
	}
	return errors.NewCode(errors.Dependency, "circular dependencies detected in DI container").
		WithContext("cycles", diag.CircularDependencies)
}
