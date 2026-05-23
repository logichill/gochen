package ident

import "gochen/ident/snowflake"

type int64Func func() (int64, error)

// Next 推进到下一项并返回是否成功。
func (f int64Func) Next() (int64, error) { return f() }

// DefaultInt64Generator 返回默认的 int64 ID 生成器实现。
//
// 说明：
// - 当前默认策略：Snowflake（`generator/snowflake.NextID`）。
// - 调用方可在组合根注入自己的实现以替换该默认行为。
func DefaultInt64Generator() IGenerator[int64] {
	return int64Func(snowflake.NextID)
}
