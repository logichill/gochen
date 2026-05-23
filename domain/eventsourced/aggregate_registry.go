package eventsourced

import (
	"reflect"
	"sync"

	"gochen/domain"
	"gochen/errors"
)

// MetadataRegistration 描述一个需要在装配期校验 metadata 的聚合。
type MetadataRegistration struct {
	Sample        any
	AggregateType string
	Error         error // 若在构造配置时提取 tag 失败，暂存此报错等到装配阶段统一抛出
}

// Metadata 聚合元数据（预编译）。
//
// 在构造/装配阶段通过反射扫描聚合的事件处理方法，
// 运行时直接使用，无需业务侧手写 BindMetadata 样板。
type Metadata struct {
	AggregateType string
	TargetType    reflect.Type
	Handlers      map[reflect.Type]*EventHandler // eventGoType -> handler
}

// EventHandler 预编译的事件处理器。
type EventHandler struct {
	Method    reflect.Method
	HasError  bool // 方法是否返回 error
	ParamType reflect.Type
}

type metadataKey struct {
	AggregateType string
	TargetType    reflect.Type
}

// MetadataRegistry 管理事件溯源聚合 metadata 缓存。
//
// 每个应用运行时、Host 或测试用例应显式持有自己的 registry，避免进程级全局状态污染。
type MetadataRegistry struct {
	mu            sync.RWMutex
	metadataByKey map[metadataKey]*Metadata
}

// NewMetadataRegistry 创建空的聚合 metadata registry。
func NewMetadataRegistry() *MetadataRegistry {
	return &MetadataRegistry{
		metadataByKey: make(map[metadataKey]*Metadata),
	}
}

// Register 扫描并注册聚合 metadata。
func (r *MetadataRegistry) Register(sample any, aggregateType string) (*Metadata, error) {
	if r == nil {
		return nil, errors.NewCode(errors.InvalidInput, "metadata registry cannot be nil")
	}
	key, err := buildMetadataKey(sample, aggregateType)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.metadataByKey == nil {
		r.metadataByKey = make(map[metadataKey]*Metadata)
	}
	if existing, ok := r.metadataByKey[key]; ok {
		return existing, nil
	}

	metadata, err := ScanMetadata(sample, aggregateType)
	if err != nil {
		return nil, err
	}

	r.metadataByKey[key] = metadata
	return metadata, nil
}

// Ensure 返回聚合 metadata；若尚未注册则自动扫描并写入注册表。
func (r *MetadataRegistry) Ensure(sample any, aggregateType string) (*Metadata, error) {
	return r.Register(sample, aggregateType)
}

// Resolve 从注册表解析聚合 metadata。
func (r *MetadataRegistry) Resolve(sample any, aggregateType string) (*Metadata, error) {
	if r == nil {
		return nil, errors.NewCode(errors.InvalidInput, "metadata registry cannot be nil")
	}
	key, err := buildMetadataKey(sample, aggregateType)
	if err != nil {
		return nil, err
	}
	if metadata, ok := r.lookup(key); ok {
		return metadata, nil
	}

	return nil, errors.NewCode(errors.Internal, "aggregate metadata is not registered").
		WithContext("aggregate_type", aggregateType).
		WithContext("target_type", key.TargetType.String())
}

func (r *MetadataRegistry) lookup(key metadataKey) (*Metadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metadata, ok := r.metadataByKey[key]
	return metadata, ok
}

func buildMetadataKey(sample any, aggregateType string) (metadataKey, error) {
	if aggregateType == "" {
		return metadataKey{}, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}

	targetType := reflect.TypeOf(sample)
	if targetType == nil {
		return metadataKey{}, errors.NewCode(errors.InvalidInput, "aggregate sample cannot be nil").
			WithContext("aggregate_type", aggregateType)
	}

	return metadataKey{
		AggregateType: aggregateType,
		TargetType:    targetType,
	}, nil
}

// ScanMetadata 扫描聚合类型的事件处理方法，返回预编译元数据。
func ScanMetadata(sample any, aggregateType string) (*Metadata, error) {
	if sample == nil {
		return nil, errors.NewCode(errors.InvalidInput, "aggregate sample cannot be nil").
			WithContext("aggregate_type", aggregateType)
	}
	targetType := reflect.TypeOf(sample)

	handlers, err := discoverEventHandlers(targetType)
	if err != nil {
		var appErr *errors.AppError
		if errors.As(err, &appErr) && appErr != nil {
			return nil, appErr.WithContext("aggregate_type", aggregateType)
		}
		return nil, err
	}

	meta := &Metadata{
		AggregateType: aggregateType,
		TargetType:    targetType,
		Handlers:      handlers,
	}

	return meta, nil
}

// ValidateMetadataSet 仅校验 aggregate metadata，不写入注册表。
func ValidateMetadataSet(registrations ...MetadataRegistration) error {
	for i, registration := range registrations {
		if registration.Sample == nil {
			continue
		}
		if registration.Error != nil {
			return errors.Wrap(registration.Error, errors.InvalidInput, "validate aggregate metadata set").
				WithContext("index", i).
				WithContext("aggregate_type", registration.AggregateType)
		}
		if _, err := ScanMetadata(registration.Sample, registration.AggregateType); err != nil {
			var appErr *errors.AppError
			if errors.As(err, &appErr) && appErr != nil {
				return appErr.Wrap("validate aggregate metadata set").
					WithContext("index", i).
					WithContext("aggregate_type", registration.AggregateType)
			}
			return errors.Wrap(err, errors.InvalidInput, "validate aggregate metadata set").
				WithContext("index", i).
				WithContext("aggregate_type", registration.AggregateType)
		}
	}
	return nil
}

// RegisterSet 批量扫描并注册聚合 metadata。
func (r *MetadataRegistry) RegisterSet(registrations ...MetadataRegistration) error {
	if r == nil {
		return errors.NewCode(errors.InvalidInput, "metadata registry cannot be nil")
	}
	for i, registration := range registrations {
		if registration.Sample == nil {
			continue
		}
		if registration.Error != nil {
			return errors.Wrap(registration.Error, errors.InvalidInput, "register aggregate metadata set").
				WithContext("index", i).
				WithContext("aggregate_type", registration.AggregateType)
		}
		if _, err := r.Register(registration.Sample, registration.AggregateType); err != nil {
			var appErr *errors.AppError
			if errors.As(err, &appErr) && appErr != nil {
				return appErr.Wrap("register aggregate metadata set").
					WithContext("index", i).
					WithContext("aggregate_type", registration.AggregateType)
			}
			return errors.Wrap(err, errors.InvalidInput, "register aggregate metadata set").
				WithContext("index", i).
				WithContext("aggregate_type", registration.AggregateType)
		}
	}
	return nil
}

// InitAggregate 创建并绑定 metadata 的事件溯源聚合根。
//
// 说明：
// - helper 优先复用 registry 中已注册 metadata；若尚未注册，会自动扫描一次并写入缓存；
// - 业务层可直接使用本 helper，避免显式维护 metadata 绑定样板；
// - 启动期显式 registry.RegisterSet / Host Aggregates 仍推荐用于 fail-fast 预热，但不再是隐藏前置条件。
func InitAggregate[T comparable](registry *MetadataRegistry, self any, id T, aggregateType string) (*EventSourcedAggregate[T], error) {
	agg := NewEventSourcedAggregate(id, aggregateType)
	meta, err := registry.Ensure(self, aggregateType)
	if err != nil {
		return nil, err
	}
	if err := agg.BindMetadata(self, meta); err != nil {
		return nil, err
	}
	return agg, nil
}

// discoverEventHandlers 发现聚合类型的所有事件处理方法。
//
// 说明：
// - 扫描规则：
// - - 方法签名：func(receiver, *EventType) 或 func(receiver, *EventType) error
// - - 参数类型必须实现 IDomainEvent 接口。
// - - 以事件的 Go 类型（参数类型）作为映射 key，避免跨版本同名 EventType() 冲突。
func discoverEventHandlers(t reflect.Type) (map[reflect.Type]*EventHandler, error) {
	handlers := make(map[reflect.Type]*EventHandler)

	if t == nil {
		return handlers, nil
	}

	domainEventInterface := reflect.TypeOf((*domain.IDomainEvent)(nil)).Elem()
	errorInterface := reflect.TypeOf((*error)(nil)).Elem()

	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		mt := method.Type

		if mt.NumIn() != 2 {
			continue
		}
		if mt.NumOut() > 1 {
			continue
		}

		paramType := mt.In(1)
		if paramType.Kind() != reflect.Ptr {
			continue
		}
		if !paramType.Implements(domainEventInterface) {
			continue
		}

		hasError := mt.NumOut() == 1 && mt.Out(0).Implements(errorInterface)
		if existing, exists := handlers[paramType]; exists {
			return nil, errors.NewCode(errors.Conflict, "duplicate event handler").
				WithContext("event_type", paramType.String()).
				WithContext("aggregate_type", t.String()).
				WithContext("existing_handler", existing.Method.Name).
				WithContext("new_handler", method.Name)
		}

		handlers[paramType] = &EventHandler{
			Method:    method,
			HasError:  hasError,
			ParamType: paramType,
		}
	}

	return handlers, nil
}

// ApplyWithMetadata 使用预编译元数据应用事件到聚合。
func ApplyWithMetadata(self any, metadata *Metadata, evt domain.IDomainEvent) (bool, error) {
	if metadata == nil || self == nil || evt == nil {
		return false, nil
	}

	eventGoType := reflect.TypeOf(evt)
	handler, ok := metadata.Handlers[eventGoType]
	if !ok {
		return false, nil
	}

	selfValue := reflect.ValueOf(self)
	evtValue := reflect.ValueOf(evt)

	if !evtValue.IsValid() || !evtValue.Type().AssignableTo(handler.ParamType) {
		return true, errors.NewCode(errors.Internal, "event type mismatch").
			WithContext("expected", handler.ParamType.String()).
			WithContext("actual", eventGoType.String())
	}
	if !selfValue.IsValid() || !selfValue.Type().AssignableTo(handler.Method.Type.In(0)) {
		return true, errors.NewCode(errors.Internal, "receiver type mismatch").
			WithContext("expected", handler.Method.Type.In(0).String()).
			WithContext("actual", selfValue.Type().String())
	}

	var (
		results  []reflect.Value
		panicVal any
		panicked bool
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicVal = r
				panicked = true
			}
		}()
		args := []reflect.Value{selfValue, evtValue}
		results = handler.Method.Func.Call(args)
	}()
	if panicked {
		return true, errors.NewCode(errors.Internal, "panic while calling event handler").
			WithContext("handler", handler.Method.Name).
			WithContext("event_type", eventGoType.String()).
			WithContext("panic", panicVal)
	}

	if handler.HasError && len(results) == 1 && !results[0].IsNil() {
		return true, results[0].Interface().(error)
	}

	return true, nil
}
