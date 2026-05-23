package query

import (
	"fmt"
	"reflect"
	"strings"

	"gochen/errors"
)

// Bound 表示区间边界。
type Bound[T any] struct {
	Value     T
	Inclusive bool
}

// Range 表示有序字段的范围查询。
type Range[T any] struct {
	Lower *Bound[T]
	Upper *Bound[T]
}

// IsZero 判断是否为空区间。
func (r Range[T]) IsZero() bool {
	return r.Lower == nil && r.Upper == nil
}

// LowerPtr 返回下界值指针；若不存在下界则返回 nil。
func (r Range[T]) LowerPtr() *T {
	if r.Lower == nil {
		return nil
	}
	value := r.Lower.Value
	return &value
}

// UpperPtr 返回上界值指针；若不存在上界则返回 nil。
func (r Range[T]) UpperPtr() *T {
	if r.Upper == nil {
		return nil
	}
	value := r.Upper.Value
	return &value
}

// LowerValue 返回下界值；若不存在下界则返回零值。
func (r Range[T]) LowerValue() T {
	var zero T
	if r.Lower == nil {
		return zero
	}
	return r.Lower.Value
}

// UpperValue 返回上界值；若不存在上界则返回零值。
func (r Range[T]) UpperValue() T {
	var zero T
	if r.Upper == nil {
		return zero
	}
	return r.Upper.Value
}

// DecodeQueryExprs 让 Range[T] 可以直接被 querybind 绑定。
func (r *Range[T]) DecodeQueryExprs(exprs []QueryExpr) error {
	parsed, ok, err := ParseRange[T](exprs)
	if err != nil {
		return err
	}
	if !ok {
		*r = Range[T]{}
		return nil
	}
	*r = parsed
	return nil
}

// RangeFor 从 QueryFilters 中提取指定字段的范围表达式。
func RangeFor[T any](filters QueryFilters, field string) (Range[T], bool, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return Range[T]{}, false, errors.NewCode(errors.InvalidInput, "query range: field is empty")
	}
	return ParseRange[T](filters.Get(field))
}

// ParseRange 把同一字段上的 QueryExpr 解析为 Range[T]。
func ParseRange[T any](exprs []QueryExpr) (Range[T], bool, error) {
	parsed, ok, err := parseQueryValueRange(exprs, true)
	if err != nil || !ok {
		return Range[T]{}, ok, err
	}
	return convertRange[T](parsed)
}

type queryValueBound struct {
	Value     QueryValue
	Inclusive bool
}

type queryValueRange struct {
	Lower *queryValueBound
	Upper *queryValueBound
}

func fieldSupportsAutoRange(field QueryField) bool {
	switch field.Type {
	case FieldTypeInt, FieldTypeFloat, FieldTypeTime:
		return true
	default:
		return false
	}
}

func validateFieldExpressions(field QueryField, exprs []QueryExpr) error {
	if len(exprs) == 0 {
		return nil
	}
	if fieldSupportsAutoRange(field) {
		_, _, err := parseQueryValueRange(exprs, false)
		if err != nil {
			return err.WithContext("field", field.Name)
		}
		return nil
	}
	if len(exprs) > 1 {
		return errors.NewCode(errors.InvalidInput, "multiple filter expressions are not supported for this field").
			WithContext("field", field.Name)
	}
	return nil
}

func parseQueryValueRange(exprs []QueryExpr, strict bool) (queryValueRange, bool, *errors.AppError) {
	if len(exprs) == 0 {
		return queryValueRange{}, false, nil
	}

	var (
		result queryValueRange
		exact  *queryValueBound
		seen   bool
	)

	for _, expr := range exprs {
		switch expr.Op {
		case FilterOpEq:
			if exact != nil || result.Lower != nil || result.Upper != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has conflicting operators")
			}
			exact = &queryValueBound{Value: expr.Value, Inclusive: true}
			seen = true
		case FilterOpGt, FilterOpGte:
			if exact != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has conflicting operators")
			}
			if result.Lower != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has duplicate lower bound")
			}
			result.Lower = &queryValueBound{Value: expr.Value, Inclusive: expr.Op == FilterOpGte}
			seen = true
		case FilterOpLt, FilterOpLte:
			if exact != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has conflicting operators")
			}
			if result.Upper != nil {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range has duplicate upper bound")
			}
			result.Upper = &queryValueBound{Value: expr.Value, Inclusive: expr.Op == FilterOpLte}
			seen = true
		default:
			if strict {
				return queryValueRange{}, false, errors.NewCode(errors.InvalidInput, "query range contains unsupported operator").WithContext("operator", string(expr.Op))
			}
		}
	}

	if !seen {
		return queryValueRange{}, false, nil
	}
	if exact != nil {
		result.Lower = exact
		result.Upper = exact
	}
	if err := validateRangeBounds(result); err != nil {
		return queryValueRange{}, false, err
	}
	return result, true, nil
}

func validateRangeBounds(r queryValueRange) *errors.AppError {
	if r.Lower == nil || r.Upper == nil {
		return nil
	}
	cmp, err := compareQueryValues(r.Lower.Value, r.Upper.Value)
	if err != nil {
		return errors.Wrap(err, errors.InvalidInput, "query range compare failed")
	}
	if cmp > 0 {
		return errors.NewCode(errors.InvalidInput, "query range lower bound is greater than upper bound")
	}
	if cmp == 0 && (!r.Lower.Inclusive || !r.Upper.Inclusive) {
		return errors.NewCode(errors.InvalidInput, "query range is empty")
	}
	return nil
}

func compareQueryValues(left, right QueryValue) (int, error) {
	if left.Type == "" || right.Type == "" {
		return 0, missingRangeValueTypeError(left.Type, right.Type)
	}
	typ := left.Type
	if left.Type != right.Type {
		return 0, errors.NewCode(errors.InvalidInput, "query range type mismatch").WithContext("left", string(left.Type)).WithContext("right", string(right.Type))
	}
	switch typ {
	case FieldTypeInt:
		switch {
		case left.Int < right.Int:
			return -1, nil
		case left.Int > right.Int:
			return 1, nil
		default:
			return 0, nil
		}
	case FieldTypeFloat:
		switch {
		case left.Float < right.Float:
			return -1, nil
		case left.Float > right.Float:
			return 1, nil
		default:
			return 0, nil
		}
	case FieldTypeTime:
		switch {
		case left.Time.Before(right.Time):
			return -1, nil
		case left.Time.After(right.Time):
			return 1, nil
		default:
			return 0, nil
		}
	case FieldTypeString, FieldTypeEnum:
		lv := left.Normalized
		if lv == "" {
			lv = left.String
		}
		rv := right.Normalized
		if rv == "" {
			rv = right.String
		}
		switch {
		case lv < rv:
			return -1, nil
		case lv > rv:
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return 0, errors.NewCode(errors.InvalidInput, "query range type is not comparable").WithContext("type", string(typ))
	}
}

func convertRange[T any](input queryValueRange) (Range[T], bool, error) {
	if input.Lower == nil && input.Upper == nil {
		return Range[T]{}, false, nil
	}
	var out Range[T]
	if input.Lower != nil {
		value, err := convertQueryValue[T](input.Lower.Value)
		if err != nil {
			return Range[T]{}, false, err
		}
		out.Lower = &Bound[T]{Value: value, Inclusive: input.Lower.Inclusive}
	}
	if input.Upper != nil {
		value, err := convertQueryValue[T](input.Upper.Value)
		if err != nil {
			return Range[T]{}, false, err
		}
		out.Upper = &Bound[T]{Value: value, Inclusive: input.Upper.Inclusive}
	}
	return out, true, nil
}

func convertQueryValue[T any](value QueryValue) (T, error) {
	var zero T
	targetType := reflect.TypeOf(zero)
	if targetType == nil {
		return zero, errors.NewCode(errors.InvalidInput, "query range target type is nil")
	}
	converted, err := convertQueryValueToType(targetType, value)
	if err != nil {
		return zero, err
	}
	return converted.Interface().(T), nil
}

func convertQueryValueToType(targetType reflect.Type, value QueryValue) (reflect.Value, error) {
	result := reflect.New(targetType).Elem()
	switch {
	case targetType == timeType:
		if value.Type != FieldTypeTime {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		result.Set(reflect.ValueOf(value.Time))
		return result, nil
	case targetType.Kind() == reflect.String:
		if value.Type != FieldTypeString && value.Type != FieldTypeEnum {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		result.SetString(value.String)
		return result, nil
	case isSignedIntKind(targetType.Kind()):
		if value.Type != FieldTypeInt {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		if err := ValidateSignedIntRange(targetType, value.Int); err != nil {
			return reflect.Value{}, err
		}
		result.SetInt(value.Int)
		return result, nil
	case isUnsignedIntKind(targetType.Kind()):
		if value.Type != FieldTypeInt {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		if err := ValidateUnsignedIntRange(targetType, value.Int); err != nil {
			return reflect.Value{}, err
		}
		result.SetUint(uint64(value.Int))
		return result, nil
	case isFloatKind(targetType.Kind()):
		if value.Type != FieldTypeFloat && value.Type != FieldTypeInt {
			return reflect.Value{}, incompatibleRangeValueError(targetType, value.Type)
		}
		if value.Type == FieldTypeInt {
			result.SetFloat(float64(value.Int))
			return result, nil
		}
		result.SetFloat(value.Float)
		return result, nil
	default:
		return reflect.Value{}, errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported query range target: %s", targetType.String()))
	}
}

func incompatibleRangeValueError(targetType reflect.Type, valueType FieldType) error {
	message := "query range: incompatible query value type"
	if valueType == "" {
		message = "query range: query value type is required"
	}
	return errors.NewCode(errors.InvalidInput, message).
		WithContext("target", targetType.String()).
		WithContext("value_type", string(valueType))
}

func missingRangeValueTypeError(leftType, rightType FieldType) error {
	return errors.NewCode(errors.InvalidInput, "query range: query value type is required").
		WithContext("left_type", string(leftType)).
		WithContext("right_type", string(rightType))
}

func isSignedIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}

func isUnsignedIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

func isFloatKind(kind reflect.Kind) bool {
	return kind == reflect.Float32 || kind == reflect.Float64
}
