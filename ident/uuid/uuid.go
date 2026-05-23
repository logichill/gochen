// Package uuid 提供 UUID 生成器实现。
//
// 基于 github.com/google/uuid 封装，适配 ident.IGenerator 接口。
package uuid

import (
	"github.com/google/uuid"

	"gochen/errors"
)

// Generator UUID 生成器。
type Generator struct{}

// NewGenerator 创建Generator。
func NewGenerator() *Generator {
	return &Generator{}
}

// Next 推进到下一项并返回是否成功。
func (g *Generator) Next() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", errors.NewCode(errors.Internal, "failed to generate UUID").
			WithContext("cause", err.Error())
	}
	return id.String(), nil
}

// 全局默认生成器。
var defaultGenerator = NewGenerator()

// New 创建字符串。
func New() (string, error) {
	return defaultGenerator.Next()
}

// NewV7 创建V7。
func NewV7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", errors.NewCode(errors.Internal, "failed to generate UUID v7").
			WithContext("cause", err.Error())
	}
	return id.String(), nil
}

// Parse 解析输入。
func Parse(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, errors.NewCode(errors.InvalidInput, "invalid UUID format").
			WithContext("cause", err.Error())
	}
	return id, nil
}

// IsValid 检查字符串是否为有效的 UUID 格式。
func IsValid(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
