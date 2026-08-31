package query

import (
	"strconv"
	"time"
)

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
