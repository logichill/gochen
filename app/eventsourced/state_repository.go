package eventsourced

import (
	"context"
	"gochen/contextx"
	"gochen/domain"
	es "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/internal/depcheck"
	"reflect"
)

// IEventStream 提供顺序回放与带乐观锁的追加；实现负责解码、升级和发布。
// ReadEvents 必须交付从版本 1 开始连续的完整事件流，失败时调用方丢弃部分状态。
// AppendEvents 的 AfterCommitError 表示已经提交，禁止当作未提交重试。
type IEventStream[ID comparable] interface {
	ReadEvents(context.Context, ID, func(domain.IDomainEvent) error) (uint64, error)
	AppendEvents(context.Context, ID, []domain.IDomainEvent, uint64) error
}

// State 保存加载/提交成功的基线；业务通过 Value 获取隔离副本。
// 不支持并发使用同一状态的可变回调；仓储不自动重试决定。
type State[S any, ID comparable] struct {
	id      ID
	version uint64
	value   S
	clone   func(S) S
	owner   any
}

// Value 返回可供业务读取和修改的独立状态副本。
func (s *State[S, ID]) Value() S { return s.clone(s.value) }

// Version 返回本次加载/提交的事件流版本。
func (s *State[S, ID]) Version() uint64 { return s.version }

// ID 返回事件流标识。
func (s *State[S, ID]) ID() ID { return s.id }

// StateRepository 编排显式状态的加载、归约、决定和追加，无聚合基类或元数据要求。
// 它是提交工具，不代替业务入口授权；业务应通过 StateApplication 访问，
// 低层 Load/Execute 仅由可信组合根持有，不提供资源授权。
type StateRepository[S any, E domain.IDomainEvent, ID comparable] struct {
	model  es.Model[S, E]
	stream IEventStream[ID]
}

// NewStateRepository 校验显式模型及事件流依赖。
func NewStateRepository[S any, E domain.IDomainEvent, ID comparable](model es.Model[S, E], stream IEventStream[ID]) (*StateRepository[S, E, ID], error) {
	if err := model.Validate(); err != nil {
		return nil, err
	}
	if depcheck.IsNil(stream) {
		return nil, errors.NewCode(errors.InvalidInput, "event stream cannot be nil")
	}
	return &StateRepository[S, E, ID]{model: model, stream: stream}, nil
}

// Load 从完整事件流重建独立状态；不存在的流返回版本零的初态。
// 具体事件类型 E 接受同一类型的值或指针表示；接口 E 保留流交付的具体类型，
// 多事件模型应按 registry 的指针事件约定产出和处理事件。
func (r *StateRepository[S, E, ID]) Load(ctx context.Context, id ID) (*State[S, ID], error) {
	if err := stateContext(ctx); err != nil {
		return nil, err
	}
	state := r.model.Clone(r.model.Initial())
	version, err := r.stream.ReadEvents(ctx, id, func(event domain.IDomainEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depcheck.IsNil(event) {
			return errors.NewCode(errors.InvalidInput, "event cannot be nil")
		}
		typed, err := stateEvent[E](event)
		if err != nil {
			return err
		}
		next, err := r.model.Reduce(state, typed)
		if err != nil {
			return err
		}
		state = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.state(id, version, state), nil
}

// stateEvent 统一相同 Go 事件类型的值/指针表示，避免内存载荷与 JSON 解码结果不同。
// 这里只复制表示，不扫描处理器、不按 EventType 或相似字段转换其他类型。
// 事件按不可变输入使用；含引用的载荷不会因此自动深拷贝。
func stateEvent[E domain.IDomainEvent](event domain.IDomainEvent) (E, error) {
	if typed, ok := event.(E); ok {
		return typed, nil
	}
	if pointer, ok := any(event).(*E); ok && pointer != nil {
		return *pointer, nil
	}
	target, value := reflect.TypeFor[E](), reflect.ValueOf(event)
	if target.Kind() == reflect.Pointer && value.IsValid() && value.Type() == target.Elem() {
		pointer := reflect.New(value.Type())
		pointer.Elem().Set(value)
		return pointer.Interface().(E), nil
	}
	var zero E
	return zero, errors.NewCode(errors.InvalidInput, "event does not belong to model")
}

// Execute 在基线的副本上决定事件，再从原基线归约事件，最后以原版本追加。
// 决定函数的状态修改不会成为结果，只有事件会改变状态。失败不改变基线；
// 成功或 AfterCommitError 返回新的基线，旧基线仍可读取但再次写入会发生版本冲突。
// 空事件不追加，也不执行版本守卫。事件不可被回调保留后修改；不自动重试。
func (r *StateRepository[S, E, ID]) Execute(ctx context.Context, base *State[S, ID], decide func(context.Context, S) ([]E, error)) (*State[S, ID], error) {
	return r.execute(ctx, base, decide, func(S) error { return nil })
}

// execute 在事件归约后、任何追加前检查真实候选状态。guard 只接收隔离副本，
// 不能通过修改校验参数改变实际提交状态；即使没有事件，也必须执行检查。
func (r *StateRepository[S, E, ID]) execute(ctx context.Context, base *State[S, ID], decide func(context.Context, S) ([]E, error), guard func(S) error) (*State[S, ID], error) {
	if err := stateContext(ctx); err != nil {
		return nil, err
	}
	if r == nil || base == nil || base.owner != r || decide == nil || guard == nil {
		return nil, errors.NewCode(errors.InvalidInput, "state must belong to repository and decide is required")
	}
	events, err := decide(ctx, base.Value())
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payloads := make([]domain.IDomainEvent, len(events))
	for i, event := range events {
		if depcheck.IsNil(event) {
			return nil, errors.NewCode(errors.InvalidInput, "event cannot be nil")
		}
		payloads[i] = event
	}
	next, err := r.model.Replay(base.value, events)
	if err != nil {
		return nil, err
	}
	if err := guard(r.model.Clone(next)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uint64(len(events)) > ^uint64(0)-base.version {
		return nil, errors.NewCode(errors.InvalidInput, "event version overflow")
	}
	if len(events) == 0 {
		return r.state(base.id, base.version, base.value), nil
	}
	err = r.stream.AppendEvents(ctx, base.id, payloads, base.version)
	if err != nil && !contextx.IsAfterCommitError(err) {
		return nil, err
	}
	return r.state(base.id, base.version+uint64(len(events)), next), err
}
func (r *StateRepository[S, E, ID]) state(id ID, version uint64, value S) *State[S, ID] {
	return &State[S, ID]{id: id, version: version, value: r.model.Clone(value), clone: r.model.Clone, owner: r}
}
func stateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	return ctx.Err()
}
