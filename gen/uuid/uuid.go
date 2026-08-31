// Package uuid 提供基于 Go 标准库 uuid 实现的 UUID v4 / v7 生成器与解析能力。
//
// 适配 gen.IGenerator 接口，无需任何第三方外部依赖。
package uuid

import (
	"uuid"

	"gochen/errors"
)

// UUID 表示 128 位 (16 字节) 的通用唯一标识符，别名自 Go 标准库 uuid.UUID。
type UUID = uuid.UUID

// Nil 表示空的零值 UUID。
var Nil = uuid.Nil()

// Generator UUID 生成器。
type Generator struct{}

// NewGenerator 创建 Generator。
func NewGenerator() *Generator {
	return &Generator{}
}

// Next 推进到下一项并返回 UUID v4 字符串。
func (g *Generator) Next() (string, error) {
	return uuid.NewV4().String(), nil
}

// New 创建 UUID v4 字符串。
func New() (string, error) {
	return uuid.NewV4().String(), nil
}

// NewString 生成 UUID v4 字符串。
func NewString() string {
	return uuid.NewV4().String()
}

// NewRandom 生成 UUID v4 实例。
func NewRandom() (UUID, error) {
	return uuid.NewV4(), nil
}

// NewV7 创建 UUID v7 字符串（时间有序，RFC 9562）。
func NewV7() (string, error) {
	return uuid.NewV7().String(), nil
}

// NewV7UUID 生成 UUID v7 实例（时间有序，RFC 9562）。
func NewV7UUID() (UUID, error) {
	return uuid.NewV7(), nil
}

// Parse 解析 UUID 字符串。
func Parse(s string) (UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return Nil, errors.NewCode(errors.InvalidInput, "invalid UUID format").
			WithContext("cause", err.Error())
	}
	return id, nil
}

// IsValid 检查字符串是否为有效的 UUID 格式。
func IsValid(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
