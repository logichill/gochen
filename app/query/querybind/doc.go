// Package querybind 提供 QueryFilters 到业务 struct 的轻量绑定能力。
//
// 约定：
//   - 默认规则只覆盖安全、可推断的场景（eq -> scalar，eq/in -> slice）；
//   - 若目标字段声明为 query.Range[T]，则 gt/gte/lt/lte/eq 会自动绑定为显式区间；
//   - 业务若需要保留 operator 语义，可让字段类型实现 Decoder；
//   - QuerySchema 仍负责字段/类型/operator 的前置校验，querybind 只做绑定；
//   - 手工构造 QueryFilters 时，QueryValue.Type 必须完整，推荐使用 query.StringValue/query.IntValue 等 helper。
package querybind
