package eventsourced

import (
	"context"
	"slices"

	"gochen/errors"
	"gochen/eventing/bus"
	"gochen/eventing/projection"
	"gochen/messaging"
)

// EventSourcedAutoRegistrar 简化事件处理器与投影注册。
type EventSourcedAutoRegistrar struct {
	eventBus            bus.IEventBus
	projectionRegistrar projection.IProjectionRegistrar
}

// NewEventSourcedAutoRegistrar 创建事件SourcedAutoRegistrar。
func NewEventSourcedAutoRegistrar(eventBus bus.IEventBus, registrar projection.IProjectionRegistrar) *EventSourcedAutoRegistrar {
	return &EventSourcedAutoRegistrar{
		eventBus:            eventBus,
		projectionRegistrar: registrar,
	}
}

// RegisterHandlers 注册事件处理器；失败时逆序回滚已取得的订阅。
// 回滚失败会返回剩余释放函数并聚合错误，调用方必须保留并重试清理。
func (r *EventSourcedAutoRegistrar) RegisterHandlers(ctx context.Context, handlers ...bus.IEventHandler) ([]messaging.UnsubscribeFunc, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if r.eventBus == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event bus is nil")
	}
	unsubs := make([]messaging.UnsubscribeFunc, 0, len(handlers))
	for _, handler := range handlers {
		if handler == nil {
			continue
		}
		unsub, err := r.eventBus.SubscribeHandler(ctx, handler)
		if unsub != nil {
			unsubs = append(unsubs, unsub)
		}
		if err != nil {
			registerErr := errors.Wrap(err, errors.Dependency, "subscribe handler failed").
				WithContext("handler", handler.HandlerName())
			var rollbackErrs []error
			var remaining []messaging.UnsubscribeFunc
			for i := len(unsubs) - 1; i >= 0; i-- {
				if rollbackErr := unsubs[i](ctx); rollbackErr != nil {
					rollbackErrs = append(rollbackErrs, errors.Wrap(rollbackErr, errors.Dependency, "rollback handler subscription failed").
						WithContext("index", i))
					remaining = append(remaining, unsubs[i])
				}
			}
			slices.Reverse(remaining)
			return remaining, errors.Join(registerErr, errors.Join(rollbackErrs...))
		}
	}
	return unsubs, nil
}

// UnregisterHandlers 逆序尝试全部释放函数，聚合错误后返回。
func (r *EventSourcedAutoRegistrar) UnregisterHandlers(ctx context.Context, unsubs ...messaging.UnsubscribeFunc) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	var errs []error
	for i := len(unsubs) - 1; i >= 0; i-- {
		if unsubs[i] == nil {
			continue
		}
		if err := unsubs[i](ctx); err != nil {
			errs = append(errs, errors.Wrap(err, errors.Dependency, "unsubscribe handler failed").
				WithContext("index", i))
		}
	}
	return errors.Join(errs...)
}

// RegisterProjections 注册投影，返回绑定各次注册的释放函数。
// 出错时仍返回此前成功及本次待清理注册的释放函数，调用方必须保留并执行清理。
func (r *EventSourcedAutoRegistrar) RegisterProjections(ctx context.Context, projections ...any) ([]messaging.UnsubscribeFunc, error) {
	if ctx == nil {
		return nil, errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if r.projectionRegistrar == nil {
		return nil, errors.NewCode(errors.InvalidInput, "projection registrar is nil")
	}
	unsubs := make([]messaging.UnsubscribeFunc, 0, len(projections))
	for _, proj := range projections {
		if proj == nil {
			continue
		}
		unsub, err := r.projectionRegistrar.RegisterProjectionAny(ctx, proj)
		if unsub != nil {
			unsubs = append(unsubs, unsub)
		}
		if err != nil {
			return unsubs, errors.Wrap(err, errors.Dependency, "register projection failed").
				WithContext("projection", projectionName(proj))
		}
	}
	return unsubs, nil
}

// UnregisterProjections 逆序尝试释放全部投影并聚合错误；失败时可重试同一组释放函数。
func (r *EventSourcedAutoRegistrar) UnregisterProjections(ctx context.Context, unsubs ...messaging.UnsubscribeFunc) error {
	return r.UnregisterHandlers(ctx, unsubs...)
}

func projectionName(proj any) string {
	named, ok := proj.(interface {
		Name() string
	})
	if !ok || named == nil {
		return ""
	}
	return named.Name()
}
