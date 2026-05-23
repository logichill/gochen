// Package query 提供基础设施层可复用的查询模型与适配层 DSL。
//
// 设计约定：
//   - adapter 层只负责解析字符串 DSL（Filter）；
//   - application / repository 主路径统一消费 QueryRequest / QueryOptions；
//   - Filters 在进入主路径前会被收口为 QueryFilters（按字段归组的 map-like 结构），避免再引入中间 Criteria 视图；
//   - QueryValue 是内部 typed query 模型；手工构造 QueryFilters 时应使用 StringValue/IntValue/BoolValue/... 这类 helper，避免遗漏 Type；
//   - 对 time/int/float 这类有序字段，可通过 ParseRange/RangeFor 把同字段上的 gt/gte/lt/lte/eq 表达式收口为显式 Range；
//   - 若需要把动态字段过滤升级为“声明式 DSL”，可结合 QuerySchema/QueryField 声明字段类型、允许操作符与排序/投影能力；
//   - 若希望减少样板，也可通过 InferQuerySchema[T](...) 从 struct + query tag 自动推导 QuerySchema；
//   - 若业务需要保留 `eq/like` 等 operator 语义并在内存/自定义仓储侧复用，可直接使用 `querymatch.Text` / `querymatch.Prefix`。
//
// 安全说明：
// - 字段白名单/注入安全由仓储实现负责（例如 db/orm/repo 的 isAllowedField 校验）；
// - 本包表达协议，并提供可选的 QuerySchema/DecodeFilters 用于前置字段合法性判断与基础类型解码。
package query
