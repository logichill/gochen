package logging

import (
	"time"
)

// Field 日志字段，用于在日志中附加结构化的键值对信息。
type Field struct {
	Key   string
	Value any
}

// String 构造字符串类型的日志字段。
func String(key, value string) Field { return Field{Key: key, Value: value} }

// Int 构造 int 类型的日志字段。
func Int(key string, value int) Field { return Field{Key: key, Value: value} }

// Int64 构造 int64 类型的日志字段。
func Int64(key string, value int64) Field { return Field{Key: key, Value: value} }

// Uint64 构造 uint64 类型的日志字段。
func Uint64(key string, value uint64) Field { return Field{Key: key, Value: value} }

// Float64 构造 float64 类型的日志字段。
func Float64(key string, value float64) Field { return Field{Key: key, Value: value} }

// Bool 构造 bool 类型的日志字段。
func Bool(key string, value bool) Field { return Field{Key: key, Value: value} }

// Any 构造任意值类型的日志字段。
func Any(key string, value any) Field { return Field{Key: key, Value: value} }

// Error 把错误对象放入统一的 `error` 日志字段。
func Error(err error) Field { return Field{Key: "error", Value: err} }

// Duration 构造 time.Duration 类型的日志字段。
func Duration(key string, value time.Duration) Field { return Field{Key: key, Value: value} }
