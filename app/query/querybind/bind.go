package querybind

import (
	"reflect"
	"strings"

	"gochen/app/query"
	"gochen/errors"
)

var decoderType = reflect.TypeOf((*IDecoder)(nil)).Elem()

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

		// 匿名嵌入结构体：与 InferQuerySchema 的展开契约对齐，递归绑定其字段，
		// 否则 BaseQuery / TenantScope 这类组合复用模式的过滤字段会全部丢失。
		// 注意：嵌入字段本身通常不可寻址赋值（CanSet=false），但其内部导出字段
		// 可以设置，因此必须在 CanSet 检查之前处理。
		if fieldType.Anonymous && fieldValue.Kind() == reflect.Struct {
			// 未导出匿名嵌入字段无法反射赋值，跳过（与 struct 字段可见性规则一致）。
			if fieldType.PkgPath != "" || !fieldValue.CanAddr() {
				continue
			}
			if err := bindWithSchemaInferOptions(filters, fieldValue.Addr().Interface(), opts); err != nil {
				return err
			}
			continue
		}

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

		// Validate default pointer traversal before resolveDecoder can allocate a
		// nil pointer. Custom pointer decoders retain priority and may opt into
		// their own recursive representation.
		if fieldValue.Kind() == reflect.Ptr && !fieldValue.Type().Implements(decoderType) {
			if err := validateBindPointerType(fieldValue.Type(), map[reflect.Type]bool{}); err != nil {
				return bindError(filterName, err)
			}
		}

		// 非解码器类型直接交给 bindDefault，避免为不实现 IDecoder 的 nil 指针
		// 做无谓分配。
		if !hasDecoderCapability(fieldValue) {
			if err := bindDefault(fieldValue, exprs); err != nil {
				return bindError(filterName, err)
			}
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
		// nil 指针：先用指针类型（fieldValue.Type() 即 *T）的接口方法集
		// 静态探测是否实现 IDecoder，完全不分配；确认实现后再实例化写入。
		decoderType := reflect.TypeOf((*IDecoder)(nil)).Elem()
		if !fieldValue.Type().Implements(decoderType) {
			return nil, false
		}
		allocated := reflect.New(fieldValue.Type().Elem())
		decoder, ok := allocated.Interface().(IDecoder)
		if !ok {
			// 探测失败：不得把零值实例留给目标 DTO（绑定失败仍会返回错误，
			// 但调用方不应看到被污染的指针字段）。
			return nil, false
		}
		fieldValue.Set(allocated)
		return decoder, true
	}
	decoder, ok := fieldValue.Interface().(IDecoder)
	return decoder, ok
}

// hasDecoderCapability 静态探测字段类型（或其地址）是否实现 IDecoder，不做任何分配。
//
// 用于在给 nil 指针分配实例之前预判：只有确定实现 IDecoder 才值得分配，
// 避免绑定失败时污染目标 DTO 为非零空指针，也避免无谓堆分配。
func hasDecoderCapability(fieldValue reflect.Value) bool {
	if fieldValue.CanAddr() && fieldValue.Addr().Type().Implements(decoderType) {
		return true
	}
	if fieldValue.Type().Implements(decoderType) {
		return true
	}
	if fieldValue.Kind() == reflect.Ptr && fieldValue.Type().Elem().Implements(decoderType) {
		return true
	}
	return false
}
