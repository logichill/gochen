package ident

import "gochen/ident/uuid"

type stringFunc func() (string, error)

// Next 推进到下一项并返回是否成功。
func (f stringFunc) Next() (string, error) { return f() }

// DefaultStringGenerator 返回默认的 string ID 生成器实现。
//
// 说明：
// - 当前默认策略：UUID v4。
// - 调用方可在组合根注入自己的实现以替换该默认行为。
func DefaultStringGenerator() IGenerator[string] {
	return stringFunc(uuid.New)
}
