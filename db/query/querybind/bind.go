package querybind

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"gochen/db/query"
	"gochen/errors"
)

var timeType = reflect.TypeOf(time.Time{})

// IDecoder 表示业务自定义规则的扩展点。
//
// 设计约束：
//   - 默认规则只负责安全、可推断的绑定（eq 到标量，in 到切片）；
//   - 若业务需要保留更复杂的 operator 语义（如 eq|like、eq|is_null|not_null），
//     则由业务类型实现 IDecoder。
type IDecoder interface {
	DecodeQueryExprs(exprs []query.QueryExpr) error
}

// Bind 按默认规则 + 自定义 IDecoder，把 QueryFilters 绑定到目标 struct。
func Bind(filters query.QueryFilters, dst any) error {
	return bindWithSchemaInferOptions(filters, dst, nil)
}

func bindWithSchemaInferOptions(filters query.QueryFilters, dst any, opts *query.SchemaInferOptions) error {
	if dst == nil {
		return errors.NewCode(errors.InvalidInput, "querybind: dst is nil")
	}

	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.NewCode(errors.InvalidInput, "querybind: dst must be a non-nil pointer")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return errors.NewCode(errors.InvalidInput, "querybind: dst must point to a struct")
	}

	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		fieldValue := rv.Field(i)
		fieldType := rt.Field(i)
		if !fieldValue.CanSet() {
			continue
		}

		filterName, err := resolveFilterName(fieldType, opts)
		if err != nil {
			return errors.Wrap(err, errors.InvalidInput, "querybind: invalid filter mapping").
				WithContext("field", fieldType.Name)
		}
		if filterName == "" {
			continue
		}
		exprs := filters.Get(filterName)
		if len(exprs) == 0 {
			continue
		}

		if decoder, ok := resolveDecoder(fieldValue); ok {
			if err := decoder.DecodeQueryExprs(exprs); err != nil {
				return bindError(filterName, err)
			}
			continue
		}

		if err := bindDefault(fieldValue, exprs); err != nil {
			return bindError(filterName, err)
		}
	}

	return nil
}

// Decode 把 QueryFilters 绑定到目标类型并返回结果值。
func Decode[T any](filters query.QueryFilters) (T, error) {
	return decodeWithSchemaInferOptions[T](filters, nil)
}

func decodeWithSchemaInferOptions[T any](filters query.QueryFilters, opts *query.SchemaInferOptions) (T, error) {
	var dst T
	if err := bindWithSchemaInferOptions(filters, &dst, opts); err != nil {
		var zero T
		return zero, err
	}
	return dst, nil
}

func bindError(field string, err error) error {
	if err == nil {
		return nil
	}
	return errors.Wrap(err, errors.InvalidInput, "querybind decode failed").WithContext("field", field)
}

func resolveFilterName(field reflect.StructField, opts *query.SchemaInferOptions) (string, error) {
	tag := strings.TrimSpace(field.Tag.Get("filter"))
	if tag == "-" {
		return "", nil
	}
	if tag != "" {
		return tag, nil
	}
	name, _, err := query.ResolveQueryFieldNameWithOptions(field, opts)
	if err != nil {
		return "", err
	}
	return name, nil
}

func resolveDecoder(fieldValue reflect.Value) (IDecoder, bool) {
	if fieldValue.CanAddr() {
		if decoder, ok := fieldValue.Addr().Interface().(IDecoder); ok {
			return decoder, true
		}
	}
	if fieldValue.Kind() != reflect.Ptr {
		return nil, false
	}
	if fieldValue.IsNil() {
		fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
	}
	decoder, ok := fieldValue.Interface().(IDecoder)
	return decoder, ok
}

func bindDefault(dst reflect.Value, exprs []query.QueryExpr) error {
	if len(exprs) == 0 {
		return nil
	}

	if dst.Kind() == reflect.Ptr {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return bindDefault(dst.Elem(), exprs)
	}

	if dst.Kind() == reflect.Slice {
		return bindSlice(dst, exprs)
	}

	if len(exprs) != 1 {
		return errors.NewCode(errors.InvalidInput, "default binding expects a single query expression")
	}
	if exprs[0].Op != query.FilterOpEq {
		return errors.NewCode(errors.InvalidInput, "default binding only supports eq operator").
			WithContext("operator", string(exprs[0].Op))
	}

	return assignScalar(dst, exprs[0].Value)
}

func bindSlice(dst reflect.Value, exprs []query.QueryExpr) error {
	if len(exprs) != 1 {
		return errors.NewCode(errors.InvalidInput, "slice binding expects a single query expression")
	}
	expr := exprs[0]
	if expr.Op != query.FilterOpEq && expr.Op != query.FilterOpIn {
		return errors.NewCode(errors.InvalidInput, "slice binding only supports eq/in operator").
			WithContext("operator", string(expr.Op))
	}

	values := expr.Values
	if expr.Op == query.FilterOpEq {
		values = []query.QueryValue{expr.Value}
	}

	result := reflect.MakeSlice(dst.Type(), 0, len(values))
	for _, value := range values {
		elem := reflect.New(dst.Type().Elem()).Elem()
		if err := assignScalar(elem, value); err != nil {
			return err
		}
		result = reflect.Append(result, elem)
	}
	dst.Set(result)
	return nil
}

func assignScalar(dst reflect.Value, value query.QueryValue) error {
	targetType := dst.Type()
	switch {
	case targetType == timeType:
		if value.Type != query.FieldTypeTime {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		dst.Set(reflect.ValueOf(value.Time))
		return nil
	case targetType.Kind() == reflect.String:
		if value.Type != query.FieldTypeString && value.Type != query.FieldTypeEnum {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		dst.SetString(value.String)
		return nil
	case isSignedInt(targetType.Kind()):
		if value.Type != query.FieldTypeInt {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		if err := query.ValidateSignedIntRange(targetType, value.Int); err != nil {
			return err
		}
		dst.SetInt(value.Int)
		return nil
	case isUnsignedInt(targetType.Kind()):
		if value.Type != query.FieldTypeInt {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		if err := query.ValidateUnsignedIntRange(targetType, value.Int); err != nil {
			return err
		}
		dst.SetUint(uint64(value.Int))
		return nil
	case isFloat(targetType.Kind()):
		if value.Type != query.FieldTypeFloat && value.Type != query.FieldTypeInt {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		if value.Type == query.FieldTypeInt {
			dst.SetFloat(float64(value.Int))
			return nil
		}
		dst.SetFloat(value.Float)
		return nil
	case targetType.Kind() == reflect.Bool:
		if value.Type != query.FieldTypeBool {
			return incompatibleScalarValueError(targetType, value.Type)
		}
		dst.SetBool(value.Bool)
		return nil
	default:
		return errors.NewCode(errors.InvalidInput, fmt.Sprintf("unsupported default bind target: %s", targetType.String()))
	}
}

func incompatibleScalarValueError(targetType reflect.Type, valueType query.FieldType) error {
	message := "querybind: incompatible query value type"
	if valueType == "" {
		message = "querybind: query value type is required"
	}
	return errors.NewCode(errors.InvalidInput, message).
		WithContext("target", targetType.String()).
		WithContext("value_type", string(valueType))
}

func isSignedInt(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}

func isUnsignedInt(kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

func isFloat(kind reflect.Kind) bool {
	return kind == reflect.Float32 || kind == reflect.Float64
}
