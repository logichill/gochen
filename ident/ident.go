// Package ident 提供 ID 生成抽象与默认实现入口。
package ident

// IGenerator 生成指定类型的 ID。
//
// 设计目标：
// - 让默认仓储/一键装配路径不绑定具体实现（Snowflake/UUID/业务编码等）；
// - 由组合根显式注入具体实现，满足不同业务的 ID 策略。
type IGenerator[T comparable] interface {
	Next() (T, error)
}
