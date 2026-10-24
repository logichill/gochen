package eventsourced

import (
	"reflect"
	"strings"
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

// Metadata 聚合元数据（构造后只读）。
//
// 只能通过 ScanMetadata 或 MetadataRegistry 构造；零值不可用于绑定。
// 构造/装配阶段缓存反射发现结果，运行时仍通过反射调用处理器。
type Metadata struct {
	aggregateType string
	targetType    reflect.Type
	handlers      map[reflect.Type]eventHandler // eventGoType -> handler
}

type eventHandler struct {
	method   reflect.Method
	hasError bool
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
	if strings.TrimSpace(aggregateType) == "" {
		return metadataKey{}, errors.NewCode(errors.InvalidInput, "aggregate type cannot be empty")
	}

	targetType := reflect.TypeOf(sample)
	if targetType == nil {
		return metadataKey{}, errors.NewCode(errors.InvalidInput, "aggregate sample cannot be nil").
			WithContext("aggregate_type", aggregateType)
	}
	if targetType.Kind() != reflect.Ptr || targetType.Elem().Kind() != reflect.Struct {
		return metadataKey{}, errors.NewCode(errors.InvalidInput, "aggregate sample must be a pointer to struct").
			WithContext("aggregate_type", aggregateType).
			WithContext("target_type", targetType.String())
	}

	return metadataKey{
		AggregateType: aggregateType,
		TargetType:    targetType,
	}, nil
}

// ScanMetadata 扫描聚合的 ApplyXxx 事件处理方法，返回只读元数据。
// sample 仅用于获取类型，允许指向聚合结构体的 typed nil。
func ScanMetadata(sample any, aggregateType string) (*Metadata, error) {
	key, err := buildMetadataKey(sample, aggregateType)
	if err != nil {
		return nil, err
	}

	handlers, err := discoverEventHandlers(key.TargetType)
	if err != nil {
		var appErr *errors.AppError
		if errors.As(err, &appErr) && appErr != nil {
			return nil, appErr.WithContext("aggregate_type", aggregateType)
		}
		return nil, err
	}

	meta := &Metadata{
		aggregateType: aggregateType,
		targetType:    key.TargetType,
		handlers:      handlers,
	}

	return meta, nil
}

// ValidateMetadataSet 仅校验 aggregate metadata，不写入注册表。
func ValidateMetadataSet(registrations ...MetadataRegistration) error {
	for i, registration := range registrations {
		if registration.Error != nil {
			return errors.Wrap(registration.Error, errors.InvalidInput, "validate aggregate metadata set").
				WithContext("index", i).
				WithContext("aggregate_type", registration.AggregateType)
		}
		if registration.Sample == nil {
			if strings.TrimSpace(registration.AggregateType) != "" && registration.AggregateType != "ignored" {
				return errors.NewCode(errors.InvalidInput, "aggregate sample is required").
					WithContext("index", i).
					WithContext("aggregate_type", registration.AggregateType)
			}
			continue
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
		if registration.Error != nil {
			return errors.Wrap(registration.Error, errors.InvalidInput, "register aggregate metadata set").
				WithContext("index", i).
				WithContext("aggregate_type", registration.AggregateType)
		}
		if registration.Sample == nil {
			if strings.TrimSpace(registration.AggregateType) != "" && registration.AggregateType != "ignored" {
				return errors.NewCode(errors.InvalidInput, "aggregate sample is required").
					WithContext("index", i).
					WithContext("aggregate_type", registration.AggregateType)
			}
			continue
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

// discoverEventHandlers 仅发现 ApplyXxx 方法；框架的两个应用入口不参与扫描。
// 所有候选方法必须满足签名约定，以事件 Go 类型为 key 区分同名事件的不同版本。
func discoverEventHandlers(t reflect.Type) (map[reflect.Type]eventHandler, error) {
	handlers := make(map[reflect.Type]eventHandler)
	domainEventInterface := reflect.TypeFor[domain.IDomainEvent]()
	errorInterface := reflect.TypeFor[error]()

	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		mt := method.Type
		if !strings.HasPrefix(method.Name, "Apply") || method.Name == "ApplyEvent" || method.Name == "ApplyAndRecord" {
			continue
		}
		// *T 的反射方法也包含值接收器的包装方法，不能仅检查 mt.In(0)。
		// 限定为指针方法集独有的方法；指针/接口嵌入的提升方法须显式转发，
		// 避免把外层同名值方法误判为内层指针方法。
		if _, callableOnValue := t.Elem().MethodByName(method.Name); callableOnValue {
			return nil, errors.NewCode(errors.InvalidInput, "event handler must be declared on the aggregate pointer receiver; embedded/promoted methods are not supported and must be forwarded explicitly").
				WithContext("handler", method.Name).
				WithContext("target_type", t.String())
		}
		if method.Name == "Apply" || mt.IsVariadic() || mt.NumIn() != 2 {
			return nil, errors.NewCode(errors.InvalidInput, "event handler must be ApplyXxx with exactly one event parameter").
				WithContext("handler", method.Name).
				WithContext("target_type", t.String()).
				WithContext("signature", mt.String())
		}

		paramType := mt.In(1)
		if paramType.Kind() != reflect.Ptr || !paramType.Implements(domainEventInterface) {
			return nil, errors.NewCode(errors.InvalidInput, "event handler parameter must be a pointer implementing IDomainEvent").
				WithContext("handler", method.Name).
				WithContext("target_type", t.String()).
				WithContext("event_type", paramType.String())
		}

		if mt.NumOut() > 1 || (mt.NumOut() == 1 && mt.Out(0) != errorInterface) {
			return nil, errors.NewCode(errors.InvalidInput, "event handler must return error or nothing").
				WithContext("handler", method.Name).
				WithContext("target_type", t.String()).
				WithContext("event_type", paramType.String()).
				WithContext("signature", mt.String())
		}
		if existing, exists := handlers[paramType]; exists {
			return nil, errors.NewCode(errors.Conflict, "duplicate event handler").
				WithContext("event_type", paramType.String()).
				WithContext("target_type", t.String()).
				WithContext("existing_handler", existing.method.Name).
				WithContext("new_handler", method.Name)
		}

		handlers[paramType] = eventHandler{
			method:   method,
			hasError: mt.NumOut() == 1,
		}
	}

	return handlers, nil
}

// ApplyWithMetadata 使用缓存的反射元数据应用事件到聚合。
// 仅在没有对应处理器时返回 (false, nil)；nil 输入或零值 metadata 返回 InvalidInput。
func ApplyWithMetadata(self any, metadata *Metadata, evt domain.IDomainEvent) (bool, error) {
	if isNil(evt) {
		return false, errors.NewCode(errors.InvalidInput, "event cannot be nil")
	}
	if isNil(self) {
		return false, errors.NewCode(errors.InvalidInput, "aggregate self cannot be nil")
	}
	if metadata == nil || metadata.targetType == nil {
		return false, errors.NewCode(errors.InvalidInput, "aggregate metadata must be constructed by ScanMetadata or MetadataRegistry")
	}

	eventGoType := reflect.TypeOf(evt)
	handler, ok := metadata.handlers[eventGoType]
	if !ok {
		return false, nil
	}

	selfValue := reflect.ValueOf(self)
	evtValue := reflect.ValueOf(evt)

	if selfValue.Type() != metadata.targetType {
		return true, errors.NewCode(errors.Internal, "receiver type mismatch").
			WithContext("expected", metadata.targetType.String()).
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
		results = handler.method.Func.Call(args)
	}()
	if panicked {
		return true, errors.NewCode(errors.Internal, "panic while calling event handler").
			WithContext("handler", handler.method.Name).
			WithContext("event_type", eventGoType.String()).
			WithContext("panic", panicVal)
	}

	if handler.hasError && !results[0].IsNil() {
		return true, results[0].Interface().(error)
	}

	return true, nil
}
