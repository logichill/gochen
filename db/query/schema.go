package query

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"gochen/errors"
)

// FieldType 表示查询字段的值类型。
type FieldType string

const (
	// FieldTypeString 表示普通字符串。
	FieldTypeString FieldType = "string"
	// FieldTypeEnum 表示枚举值（仍以字符串传输）。
	FieldTypeEnum FieldType = "enum"
	// FieldTypeInt 表示整型值。
	FieldTypeInt FieldType = "int"
	// FieldTypeFloat 表示浮点值。
	FieldTypeFloat FieldType = "float"
	// FieldTypeBool 表示布尔值。
	FieldTypeBool FieldType = "bool"
	// FieldTypeTime 表示时间值。
	FieldTypeTime FieldType = "time"
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

// QuerySchema 表示动态查询 DSL 的字段能力声明。
type QuerySchema struct {
	fields map[string]QueryField
}

// QueryValue 表示按字段 schema 解码后的查询值。
//
// 约定：
//   - Type 是内部 typed query 流转的必填字段；
//   - 业务若需要手工构造 QueryFilters，应优先使用 StringValue/EnumValue/IntValue/FloatValue/BoolValue/TimeValue。
type QueryValue struct {
	Type       FieldType
	Normalized string
	String     string
	Int        int64
	Float      float64
	Bool       bool
	Time       time.Time
}

// Any 返回 QueryValue 的强类型值。
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

// NewQuerySchema 创建查询字段 schema。
func NewQuerySchema(fields ...QueryField) *QuerySchema {
	schema := &QuerySchema{
		fields: make(map[string]QueryField, len(fields)),
	}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		field.Name = name
		if len(field.FilterOps) == 0 {
			field.FilterOps = defaultFilterOps(field.Type)
		} else {
			field.FilterOps = cloneFilterOps(field.FilterOps)
		}
		schema.fields[name] = field
	}
	if len(schema.fields) == 0 {
		return nil
	}
	return schema
}

func (s *QuerySchema) Field(name string) (QueryField, bool) {
	if s == nil {
		return QueryField{}, false
	}
	field, ok := s.fields[strings.TrimSpace(name)]
	return field, ok
}

// NormalizeFilters 校验并规范化过滤条件。
func (s *QuerySchema) NormalizeFilters(filters []Filter) ([]Filter, error) {
	if len(filters) == 0 || s == nil {
		return filters, nil
	}
	out := make([]Filter, 0, len(filters))
	for _, filter := range filters {
		field, ok := s.Field(filter.Field)
		if !ok {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter field").
				WithContext("field", filter.Field)
		}
		if !field.supportsFilterOp(filter.Op) {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter operator").
				WithContext("field", filter.Field).
				WithContext("operator", string(filter.Op))
		}

		normalized := filter
		switch filter.Op {
		case FilterOpIsNull, FilterOpNotNull:
			normalized.Value = ""
			normalized.Values = nil
		case FilterOpIn, FilterOpNotIn:
			values, _, err := field.normalizeValues(filter.Values)
			if err != nil {
				return nil, err
			}
			normalized.Value = ""
			normalized.Values = values
		default:
			value, _, err := field.normalizeValue(filter.Value)
			if err != nil {
				return nil, err
			}
			normalized.Value = value
			normalized.Values = nil
		}
		out = append(out, normalized)
	}
	return out, nil
}

// DecodeFilters 校验过滤条件并返回带类型信息的查询表达式集合。
func (s *QuerySchema) DecodeFilters(filters []Filter) (QueryFilters, error) {
	if len(filters) == 0 || s == nil {
		return nil, nil
	}
	var out QueryFilters
	for _, filter := range filters {
		field, ok := s.Field(filter.Field)
		if !ok {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter field").
				WithContext("field", filter.Field)
		}
		if !field.supportsFilterOp(filter.Op) {
			return nil, errors.NewCode(errors.InvalidInput, "invalid filter operator").
				WithContext("field", filter.Field).
				WithContext("operator", string(filter.Op))
		}

		decoded := QueryExpr{Op: filter.Op}
		switch filter.Op {
		case FilterOpIsNull, FilterOpNotNull:
			out = out.Append(field.Name, decoded)
			continue
		case FilterOpIn, FilterOpNotIn:
			_, values, err := field.normalizeValues(filter.Values)
			if err != nil {
				return nil, err
			}
			decoded.Values = values
		default:
			_, value, err := field.normalizeValue(filter.Value)
			if err != nil {
				return nil, err
			}
			decoded.Value = value
		}
		out = out.Append(field.Name, decoded)
	}
	if out.IsZero() {
		return nil, nil
	}
	for fieldName, exprs := range out {
		field, ok := s.Field(fieldName)
		if !ok {
			continue
		}
		if err := validateFieldExpressions(field, exprs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ValidateSorts 校验排序字段能力。
func (s *QuerySchema) ValidateSorts(sorts []Sort) error {
	if len(sorts) == 0 || s == nil {
		return nil
	}
	for _, sort := range sorts {
		field, ok := s.Field(sort.Field)
		if !ok || !field.Sortable {
			return errors.NewCode(errors.InvalidInput, "invalid sort field").
				WithContext("field", sort.Field)
		}
	}
	return nil
}

// ValidateFields 校验字段投影能力。
func (s *QuerySchema) ValidateFields(fields []string) error {
	if len(fields) == 0 || s == nil {
		return nil
	}
	for _, name := range fields {
		field, ok := s.Field(name)
		if !ok || !field.Selectable {
			return errors.NewCode(errors.InvalidInput, "invalid field selection").
				WithContext("field", name)
		}
	}
	return nil
}

// FilterableFieldNames 返回 schema 中声明的可过滤字段名。
func (s *QuerySchema) FilterableFieldNames() []string {
	return s.fieldNames(func(field QueryField) bool { return len(field.FilterOps) > 0 })
}

// SortableFieldNames 返回 schema 中声明的可排序字段名。
func (s *QuerySchema) SortableFieldNames() []string {
	return s.fieldNames(func(field QueryField) bool { return field.Sortable })
}

// SelectableFieldNames 返回 schema 中声明的可投影字段名。
func (s *QuerySchema) SelectableFieldNames() []string {
	return s.fieldNames(func(field QueryField) bool { return field.Selectable })
}

func (s *QuerySchema) fieldNames(accept func(QueryField) bool) []string {
	if s == nil || len(s.fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.fields))
	for name, field := range s.fields {
		if accept != nil && !accept(field) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (f QueryField) supportsFilterOp(op FilterOp) bool {
	for _, allowed := range f.FilterOps {
		if allowed == op {
			return true
		}
	}
	return false
}

func (f QueryField) normalizeValues(values []string) ([]string, []QueryValue, error) {
	if len(values) == 0 {
		return nil, nil, errors.NewCode(errors.InvalidInput, "empty filter value list").
			WithContext("field", f.Name)
	}
	out := make([]string, 0, len(values))
	decoded := make([]QueryValue, 0, len(values))
	for _, value := range values {
		normalized, queryValue, err := f.normalizeValue(value)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, normalized)
		decoded = append(decoded, queryValue)
	}
	return out, decoded, nil
}

func (f QueryField) normalizeValue(value string) (string, QueryValue, error) {
	switch f.Type {
	case FieldTypeInt:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid int filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		normalized := strconv.FormatInt(parsed, 10)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Int: parsed}, nil
	case FieldTypeFloat:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid float filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		normalized := strconv.FormatFloat(parsed, 'g', -1, 64)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Float: parsed}, nil
	case FieldTypeBool:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid bool filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		normalized := strconv.FormatBool(parsed)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Bool: parsed}, nil
	case FieldTypeTime:
		layout := strings.TrimSpace(f.TimeLayout)
		if layout == "" {
			layout = time.RFC3339Nano
		}
		parsed, err := time.Parse(layout, strings.TrimSpace(value))
		if err != nil && f.TimeLayout == "" {
			parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(value))
		}
		if err != nil {
			return "", QueryValue{}, errors.NewCode(errors.InvalidInput, "invalid time filter value").
				WithContext("field", f.Name).
				WithContext("value", value)
		}
		if f.TimeLayout != "" {
			normalized := parsed.Format(f.TimeLayout)
			return normalized, QueryValue{Type: f.Type, Normalized: normalized, Time: parsed}, nil
		}
		normalized := parsed.Format(time.RFC3339Nano)
		return normalized, QueryValue{Type: f.Type, Normalized: normalized, Time: parsed}, nil
	default:
		return value, QueryValue{Type: f.Type, Normalized: value, String: value}, nil
	}
}
