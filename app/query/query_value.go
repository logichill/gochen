package query

import (
	"strconv"
	"time"
)

// FieldType 表示查询字段的值类型。
type FieldType string

const (
	FieldTypeString FieldType = "string"
	FieldTypeEnum   FieldType = "enum"
	FieldTypeInt    FieldType = "int"
	FieldTypeFloat  FieldType = "float"
	FieldTypeBool   FieldType = "bool"
	FieldTypeTime   FieldType = "time"
)

func (t FieldType) IsValid() bool {
	switch t {
	case FieldTypeString, FieldTypeEnum, FieldTypeInt, FieldTypeFloat, FieldTypeBool, FieldTypeTime:
		return true
	default:
		return false
	}
}

// QueryValue 表示查询中封装的条件值。
type QueryValue struct {
	Type       FieldType
	Normalized string
	String     string
	Int        int64
	Float      float64
	Bool       bool
	Time       time.Time
}

func (v QueryValue) Any() any {
	switch v.Type {
	case FieldTypeInt:
		return v.Int
	case FieldTypeFloat:
		return v.Float
	case FieldTypeBool:
		return v.Bool
	case FieldTypeTime:
		return v.Time
	default:
		return v.String
	}
}

// StringValue 构造 string 类型的 QueryValue。
func StringValue(value string) QueryValue {
	return QueryValue{
		Type:       FieldTypeString,
		Normalized: value,
		String:     value,
	}
}

// EnumValue 构造 enum 类型的 QueryValue。
func EnumValue(value string) QueryValue {
	return QueryValue{
		Type:       FieldTypeEnum,
		Normalized: value,
		String:     value,
	}
}

// IntValue 构造 int 类型的 QueryValue。
func IntValue(value int64) QueryValue {
	normalized := strconv.FormatInt(value, 10)
	return QueryValue{
		Type:       FieldTypeInt,
		Normalized: normalized,
		Int:        value,
	}
}

// FloatValue 构造 float 类型的 QueryValue。
func FloatValue(value float64) QueryValue {
	normalized := strconv.FormatFloat(value, 'g', -1, 64)
	return QueryValue{
		Type:       FieldTypeFloat,
		Normalized: normalized,
		Float:      value,
	}
}

// BoolValue 构造 bool 类型的 QueryValue。
func BoolValue(value bool) QueryValue {
	normalized := strconv.FormatBool(value)
	return QueryValue{
		Type:       FieldTypeBool,
		Normalized: normalized,
		Bool:       value,
	}
}

// TimeValue 构造 time 类型的 QueryValue。
func TimeValue(value time.Time) QueryValue {
	normalized := value.Format(time.RFC3339Nano)
	return QueryValue{
		Type:       FieldTypeTime,
		Normalized: normalized,
		Time:       value,
	}
}
