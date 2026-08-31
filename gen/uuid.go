package gen

import "gochen/gen/uuid"

// NewUUIDGenerator 创建独立的 UUID v4 生成器实例。
func NewUUIDGenerator() IGenerator[string] { return uuid.NewGenerator() }

// NewUUIDv7Generator 创建独立的 UUID v7 生成器实例（时间有序）。
func NewUUIDv7Generator() IGenerator[string] { return uuidv7Generator{} }

type uuidv7Generator struct{}

func (uuidv7Generator) Next() (string, error) {
	return uuid.NewV7()
}
