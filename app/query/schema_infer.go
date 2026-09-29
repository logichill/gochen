package query

import (
	"reflect"
	"strings"
	"time"

	"gochen/codec/casecodec"
	"gochen/errors"
)

var timeType = reflect.TypeOf(time.Time{})

// SchemaInferOptions 定义从 struct 推导 QuerySchema 的可选配置。
type SchemaInferOptions struct {
	// FieldNameMapper 用于把 struct 字段名映射为查询字段名。
	//
	// 说明：
	// - 返回空字符串时会回退到默认 snake_case；
	// - 显式 `query:"field=..."` / `query:"name=..."` 会优先覆盖该映射结果。
	FieldNameMapper func(field reflect.StructField) string
}

// InferQuerySchema 基于泛型 struct 类型推导 QuerySchema。
func InferQuerySchema[T any](opts *SchemaInferOptions) (*QuerySchema, error) {
	return InferQuerySchemaFromType(reflect.TypeFor[T](), opts)
}

// MustInferQuerySchema 基于泛型 struct 类型推导 QuerySchema，失败时直接 panic。
func MustInferQuerySchema[T any](opts *SchemaInferOptions) *QuerySchema {
	schema, err := InferQuerySchema[T](opts)
	if err != nil {
		panic(err)
	}
	return schema
}

// InferQuerySchemaFromType 基于 reflect.Type 推导 QuerySchema。
func InferQuerySchemaFromType(typ reflect.Type, opts *SchemaInferOptions) (*QuerySchema, error) {
	typ = derefQueryType(typ)
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil, errors.NewCode(errors.InvalidInput, "query schema inference requires struct type")
	}

	fields := make([]QueryField, 0)
	seen := make(map[string]struct{})
	if err := collectInferredQueryFields(typ, opts, &fields, seen, map[reflect.Type]bool{}); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, nil
	}
	schema := &QuerySchema{
		fields: make(map[string]QueryField, len(fields)),
	}
	for _, field := range fields {
		field.FilterOps = cloneFilterOps(field.FilterOps)
		schema.fields[field.Name] = field
	}
	return schema, nil
}

// InferQuerySchemaFromValue 基于 struct 实例推导 QuerySchema。
func InferQuerySchemaFromValue(value any, opts *SchemaInferOptions) (*QuerySchema, error) {
	if value == nil {
		return nil, errors.NewCode(errors.InvalidInput, "query schema inference requires non-nil value")
	}
	return InferQuerySchemaFromType(reflect.TypeOf(value), opts)
}

func collectInferredQueryFields(typ reflect.Type, opts *SchemaInferOptions, out *[]QueryField, seen map[string]struct{}, path map[reflect.Type]bool) error {
	if path[typ] {
		return errors.NewCode(errors.InvalidInput, "recursive query schema type").WithContext("type", typ.String())
	}
	path[typ] = true
	defer delete(path, typ)
	for i := 0; i < typ.NumField(); i++ {
		sf := typ.Field(i)
		if !sf.IsExported() {
			continue
		}

		tag, hasTag, err := parseInferTag(sf.Tag.Get("query"))
		if err != nil {
			return errors.Wrap(err, errors.InvalidInput, "invalid query tag").WithContext("field", sf.Name)
		}
		if tag.Skip {
			continue
		}
		// An explicit query tag keeps control over inference.  Otherwise a
		// json:"-" field is hidden before inspecting its type; hidden recursive
		// DTO fields must not make the whole query schema invalid.
		if shouldSkipInferredField(sf, hasTag) {
			continue
		}

		innerType := derefQueryType(sf.Type)
		if sf.Anonymous && tag.Name == "" && tag.Type == "" && innerType != nil && innerType.Kind() == reflect.Struct && innerType != timeType {
			if err := collectInferredQueryFields(innerType, opts, out, seen, path); err != nil {
				return err
			}
			continue
		}

		fieldType, ok, err := inferQueryFieldType(sf.Type, tag.Type)
		if err != nil {
			return errors.Wrap(err, errors.InvalidInput, "invalid inferred query field type").WithContext("field", sf.Name)
		}
		if !ok {
			continue
		}

		name := tag.Name
		if name == "" {
			name = inferFieldName(sf, opts)
		}
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			return errors.NewCode(errors.Conflict, "duplicated inferred query field").
				WithContext("field", sf.Name).
				WithContext("query_field", name)
		}

		field := QueryField{
			Name:       name,
			Type:       fieldType,
			FilterOps:  inferDefaultFilterOpsForType(innerType, fieldType),
			Sortable:   true,
			Selectable: true,
			TimeLayout: tag.TimeLayout,
		}
		if tag.NoFilter {
			field.FilterOps = nil
		}
		if tag.HasOps {
			field.FilterOps = cloneFilterOps(tag.Ops)
		}
		if tag.Sortable != nil {
			field.Sortable = *tag.Sortable
		}
		if tag.Selectable != nil {
			field.Selectable = *tag.Selectable
		}

		if len(field.FilterOps) == 0 && !field.Sortable && !field.Selectable {
			continue
		}

		seen[name] = struct{}{}
		*out = append(*out, field)
	}
	return nil
}

func shouldSkipInferredField(sf reflect.StructField, hasQueryTag bool) bool {
	if hasQueryTag {
		return false
	}
	if raw, ok := sf.Tag.Lookup("json"); ok {
		name := strings.TrimSpace(strings.Split(raw, ",")[0])
		if name == "-" {
			return true
		}
	}
	return false
}

func inferFieldName(sf reflect.StructField, opts *SchemaInferOptions) string {
	if opts != nil && opts.FieldNameMapper != nil {
		if mapped := strings.TrimSpace(opts.FieldNameMapper(sf)); mapped != "" {
			return mapped
		}
	}
	return toSnakeCase(sf.Name)
}

// ResolveQueryFieldName 返回 struct 字段对应的 query 字段名。
//
// 说明：
// - 若 `query:"field=..."` / `query:"name=..."` 存在，则优先返回显式字段名；
// - 若 `query:"-"`，则返回空字符串，表示该字段应被跳过；
// - 否则回退到默认 snake_case 字段名。
func ResolveQueryFieldName(sf reflect.StructField) (string, bool, error) {
	return ResolveQueryFieldNameWithOptions(sf, nil)
}

// ResolveQueryFieldNameWithOptions 返回 struct 字段对应的 query 字段名，并复用 schema 推导时的命名规则。
func ResolveQueryFieldNameWithOptions(sf reflect.StructField, opts *SchemaInferOptions) (string, bool, error) {
	tag, hasTag, err := parseInferTag(sf.Tag.Get("query"))
	if err != nil {
		return "", false, err
	}
	if tag.Skip {
		return "", true, nil
	}
	if tag.Name != "" {
		return tag.Name, hasTag, nil
	}
	return inferFieldName(sf, opts), hasTag, nil
}

func derefQueryType(typ reflect.Type) reflect.Type {
	seen := make(map[reflect.Type]struct{})
	for typ != nil && typ.Kind() == reflect.Pointer {
		if _, ok := seen[typ]; ok {
			return nil
		}
		seen[typ] = struct{}{}
		typ = typ.Elem()
	}
	return typ
}

func inferQueryFieldType(typ reflect.Type, explicit string) (FieldType, bool, error) {
	return inferQueryFieldTypeSeen(typ, explicit, make(map[reflect.Type]bool))
}

func inferQueryFieldTypeSeen(typ reflect.Type, explicit string, path map[reflect.Type]bool) (FieldType, bool, error) {
	if explicit != "" {
		fieldType, err := parseFieldType(explicit)
		if err != nil {
			return "", false, err
		}
		return fieldType, true, nil
	}
	if typ == nil {
		return "", false, errors.NewCode(errors.InvalidInput, "recursive query field type")
	}
	if typ == timeType {
		return FieldTypeTime, true, nil
	}
	if path[typ] {
		return "", false, errors.NewCode(errors.InvalidInput, "recursive query field type").WithContext("type", typ.String())
	}
	path[typ] = true
	defer delete(path, typ)
	// 环检测与实际推导共用路径；普通 struct、map、array 不下探。
	if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		return inferQueryFieldTypeSeen(typ.Elem(), "", path)
	}
	if valueType, ok := rangeQueryValueType(typ); ok {
		return inferQueryFieldTypeSeen(valueType, "", path)
	}

	switch typ.Kind() {
	case reflect.String:
		return FieldTypeString, true, nil
	case reflect.Bool:
		return FieldTypeBool, true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return FieldTypeInt, true, nil
	case reflect.Float32, reflect.Float64:
		return FieldTypeFloat, true, nil
	default:
		return "", false, nil
	}
}

// rangeQueryValueType 返回 Range 类型承载的取值类型。
func rangeQueryValueType(typ reflect.Type) (reflect.Type, bool) {
	typ = derefQueryType(typ)
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil, false
	}
	if typ.PkgPath() != "gochen/app/query" || !strings.HasPrefix(typ.Name(), "Range[") {
		return nil, false
	}

	lowerField, ok := typ.FieldByName("Lower")
	if !ok {
		return nil, false
	}
	lowerType := derefQueryType(lowerField.Type)
	if lowerType == nil || lowerType.Kind() != reflect.Struct || lowerType.PkgPath() != "gochen/app/query" || !strings.HasPrefix(lowerType.Name(), "Bound[") {
		return nil, false
	}

	valueField, ok := lowerType.FieldByName("Value")
	if !ok {
		return nil, false
	}
	return valueField.Type, true
}

func inferDefaultFilterOpsForType(sourceType reflect.Type, typ FieldType) []FilterOp {
	if sourceType != nil && sourceType.Kind() == reflect.Slice {
		return []FilterOp{FilterOpEq, FilterOpIn}
	}
	return inferDefaultFilterOps(typ)
}

func inferDefaultFilterOps(typ FieldType) []FilterOp {
	switch typ {
	case FieldTypeBool:
		return []FilterOp{FilterOpEq}
	case FieldTypeInt, FieldTypeFloat, FieldTypeTime:
		return []FilterOp{FilterOpEq, FilterOpGt, FilterOpGte, FilterOpLt, FilterOpLte}
	case FieldTypeEnum:
		return []FilterOp{FilterOpEq}
	default:
		return []FilterOp{FilterOpEq, FilterOpLike}
	}
}

type inferTag struct {
	Skip       bool
	Name       string
	Type       string
	TimeLayout string
	Ops        []FilterOp
	HasOps     bool
	NoFilter   bool
	Sortable   *bool
	Selectable *bool
}

func parseInferTag(raw string) (inferTag, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return inferTag{}, false, nil
	}
	if raw == "-" {
		return inferTag{Skip: true}, true, nil
	}

	tag := inferTag{}
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if key, value, ok := strings.Cut(part, "="); ok {
			key = strings.TrimSpace(strings.ToLower(key))
			value = strings.TrimSpace(value)
			switch key {
			case "field", "name":
				tag.Name = value
			case "type":
				tag.Type = strings.ToLower(value)
			case "ops":
				tag.HasOps = true
				if value == "" {
					tag.Ops = nil
					break
				}
				ops, err := parseFilterOps(value)
				if err != nil {
					return inferTag{}, true, err
				}
				tag.Ops = ops
			case "layout", "time_layout":
				tag.TimeLayout = value
			default:
				return inferTag{}, true, errors.NewCode(errors.InvalidInput, "unknown query tag option").WithContext("option", key)
			}
			continue
		}

		switch strings.ToLower(part) {
		case "sort":
			value := true
			tag.Sortable = &value
		case "nosort":
			value := false
			tag.Sortable = &value
		case "select":
			value := true
			tag.Selectable = &value
		case "noselect":
			value := false
			tag.Selectable = &value
		case "nofilter":
			tag.NoFilter = true
		default:
			return inferTag{}, true, errors.NewCode(errors.InvalidInput, "unknown query tag flag").WithContext("flag", part)
		}
	}
	return tag, true, nil
}

func parseFieldType(raw string) (FieldType, error) {
	typ := FieldType(strings.ToLower(strings.TrimSpace(raw)))
	if !typ.IsValid() {
		return "", errors.NewCode(errors.InvalidInput, "unknown query field type").WithContext("type", raw)
	}
	return typ, nil
}

func parseFilterOps(raw string) ([]FilterOp, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == '|' || r == ';' })
	ops := make([]FilterOp, 0, len(parts))
	for _, part := range parts {
		op := FilterOp(strings.TrimSpace(part))
		if !op.IsValid() {
			return nil, errors.NewCode(errors.InvalidInput, "unknown filter op").WithContext("operator", part)
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// toSnakeCase 把 Go 字段名渲染为 snake_case 查询字段名。
//
// 渲染不会失败（Encode 对任何 Words 都有定义），故此处忽略 error，
// 保持调用点的单返回值形态。
func toSnakeCase(in string) string {
	name, _ := casecodec.Convert(casecodec.Snake, in)
	return name
}
