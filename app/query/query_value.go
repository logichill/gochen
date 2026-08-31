package query

import "time"

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
