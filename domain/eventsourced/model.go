package eventsourced

import "gochen/errors"

// Reducer 显式应用一个事件；未知事件必须返回错误，不得执行外部副作用。
type Reducer[S, E any] func(S, E) (S, error)

// Model 定义状态构造、隔离副本与确定性事件归约，不要求基类或反射元数据。
// Clone 必须复制全部可变 map、slice、pointer；不可变值可直接返回。
// Initial 每次返回独立初态。回调不得保留并异步修改参数，也不得修改事件。
// Go 无法证明深拷贝，业务必须通过别名隔离测试验证 Clone；库不宣称任意 S 自动纯净。
type Model[S, E any] struct {
	Initial func() S
	Clone   func(S) S
	Reduce  Reducer[S, E]
}

// Validate 在装配期拒绝缺失的状态隔离或归约能力。
func (m Model[S, E]) Validate() error {
	if m.Initial == nil || m.Clone == nil || m.Reduce == nil {
		return errors.NewCode(errors.InvalidInput, "initial, clone and reducer are required")
	}
	return nil
}

// Replay 在独立副本上按顺序归约；失败不返回部分状态，也不修改调用方初态。
// 此保证以 Clone 的深拷贝契约为前提；事件须由调用方保持只读。
func (m Model[S, E]) Replay(initial S, events []E) (S, error) {
	var zero S
	if err := m.Validate(); err != nil {
		return zero, err
	}
	state := m.Clone(initial)
	for _, evt := range events {
		next, err := m.Reduce(state, evt)
		if err != nil {
			return zero, err
		}
		state = next
	}
	return m.Clone(state), nil
}
