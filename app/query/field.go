package query

import (
	"strings"
)

// QueryField 表示一个可声明的动态查询字段。
type QueryField struct {
	Name       string
	Type       FieldType
	FilterOps  []FilterOp
	Sortable   bool
	Selectable bool
	TimeLayout string
}

// FieldOption 表示字段声明的可选配置。
type FieldOption func(*QueryField)

// WithFilterOps 显式设置允许的过滤操作符集合。
func WithFilterOps(ops ...FilterOp) FieldOption {
	return func(field *QueryField) {
		field.FilterOps = cloneFilterOps(ops)
	}
}

// AllowSort 允许该字段参与排序。
func AllowSort() FieldOption {
	return func(field *QueryField) {
		field.Sortable = true
	}
}

// AllowSelect 允许该字段参与 fields 投影。
func AllowSelect() FieldOption {
	return func(field *QueryField) {
		field.Selectable = true
	}
}

// WithTimeLayout 指定时间字段的解析格式。
func WithTimeLayout(layout string) FieldOption {
	return func(field *QueryField) {
		field.TimeLayout = strings.TrimSpace(layout)
	}
}

// StringField 创建字符串字段声明。
func StringField(name string, options ...FieldOption) QueryField {
	return newQueryField(name, FieldTypeString, options...)
}

// EnumField 创建枚举字段声明。
func EnumField(name string, options ...FieldOption) QueryField {
	return newQueryField(name, FieldTypeEnum, options...)
}

// IntField 创建整数字段声明。
func IntField(name string, options ...FieldOption) QueryField {
	return newQueryField(name, FieldTypeInt, options...)
}

// FloatField 创建浮点字段声明。
func FloatField(name string, options ...FieldOption) QueryField {
	return newQueryField(name, FieldTypeFloat, options...)
}

// BoolField 创建布尔字段声明。
func BoolField(name string, options ...FieldOption) QueryField {
	return newQueryField(name, FieldTypeBool, options...)
}

// TimeField 创建时间字段声明。
func TimeField(name string, options ...FieldOption) QueryField {
	return newQueryField(name, FieldTypeTime, options...)
}

func newQueryField(name string, typ FieldType, options ...FieldOption) QueryField {
	field := QueryField{
		Name:      strings.TrimSpace(name),
		Type:      typ,
		FilterOps: defaultFilterOps(typ),
	}
	for _, option := range options {
		if option != nil {
			option(&field)
		}
	}
	return field
}

func cloneFilterOps(in []FilterOp) []FilterOp {
	if len(in) == 0 {
		return nil
	}
	out := make([]FilterOp, len(in))
	copy(out, in)
	return out
}

func defaultFilterOps(typ FieldType) []FilterOp {
	switch typ {
	case FieldTypeBool:
		return []FilterOp{FilterOpEq, FilterOpNe, FilterOpIn, FilterOpNotIn, FilterOpIsNull, FilterOpNotNull}
	case FieldTypeInt, FieldTypeFloat, FieldTypeTime:
		return []FilterOp{
			FilterOpEq, FilterOpNe,
			FilterOpGt, FilterOpGte,
			FilterOpLt, FilterOpLte,
			FilterOpIn, FilterOpNotIn,
			FilterOpIsNull, FilterOpNotNull,
		}
	default:
		return []FilterOp{
			FilterOpEq, FilterOpNe, FilterOpLike,
			FilterOpIn, FilterOpNotIn,
			FilterOpIsNull, FilterOpNotNull,
		}
	}
}

func (f QueryField) supportsFilterOp(op FilterOp) bool {
	for _, allowed := range f.FilterOps {
		if allowed == op {
			return true
		}
	}
	return false
}
