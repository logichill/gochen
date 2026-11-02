package meta

import (
	"reflect"
	"strconv"
	"sync"

	"gochen/errors"
)

// Field 描述实体上的一个导出字段。
type Field struct {
	Name      string
	Type      reflect.Type
	Identity  bool
	Dimension Dimension
	Managed   bool

	index []int
}

// Metadata 是实体类型的字段元数据快照。
type Metadata struct {
	typ         reflect.Type
	fields      []Field
	byName      map[string]int
	byDimension map[Dimension]int
	id          int
}

type cacheEntry struct {
	meta *Metadata
	err  error
}

var metadataCache sync.Map // reflect.Type -> cacheEntry

// Describe 返回实体类型的字段元数据。T 必须是指向结构体的指针，
// 与 D5 的构造期断言一致。
func Describe[T any]() (*Metadata, error) {
	var zero T
	return DescribeType(reflect.TypeOf(zero))
}

// DescribeValue 返回实体值对应的字段元数据。entity 必须是非空指针。
func DescribeValue(entity any) (*Metadata, error) {
	if entity == nil {
		return nil, errors.NewCode(errors.InvalidInput, "entity cannot be nil")
	}
	return DescribeType(reflect.TypeOf(entity))
}

// DescribeType 返回指定类型的字段元数据。
func DescribeType(t reflect.Type) (*Metadata, error) {
	if t == nil {
		return nil, errors.NewCode(errors.InvalidInput, "entity type cannot be nil")
	}
	if t.Kind() != reflect.Ptr || t.Elem().Kind() != reflect.Struct {
		return nil, errors.NewCode(errors.InvalidInput, "entity type must be a pointer to struct").
			WithContext("type", t.String())
	}
	elem := t.Elem()
	if cached, ok := metadataCache.Load(elem); ok {
		entry := cached.(cacheEntry)
		return entry.meta, entry.err
	}
	m, err := build(elem)
	metadataCache.Store(elem, cacheEntry{meta: m, err: err})
	return m, err
}

func build(t reflect.Type) (*Metadata, error) {
	m := &Metadata{
		typ:         t,
		byName:      make(map[string]int),
		byDimension: make(map[Dimension]int),
		id:          -1,
	}
	for _, f := range reflect.VisibleFields(t) {
		if f.Anonymous || f.PkgPath != "" {
			continue
		}
		field := Field{
			Name:  f.Name,
			Type:  f.Type,
			index: append([]int(nil), f.Index...),
		}
		if raw, tagged := f.Tag.Lookup("entity"); tagged {
			if raw != "id" {
				return nil, errors.NewCode(errors.InvalidInput, "invalid entity tag").WithContext("field", f.Name)
			}
			field.Identity = true
		}
		if raw, tagged := f.Tag.Lookup("dim"); tagged {
			d, err := ParseDimension(raw)
			if err != nil {
				return nil, err
			}
			field.Dimension = d
		}
		if raw := f.Tag.Get("managed"); raw != "" {
			managed, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, errors.NewCode(errors.InvalidInput, "invalid managed tag").
					WithContext("field", f.Name).
					WithContext("managed", raw)
			}
			field.Managed = managed
		}
		if field.Identity {
			if m.id >= 0 || field.Dimension != "" {
				return nil, errors.NewCode(errors.InvalidInput, "entity identity must have a unique binding")
			}
			m.id = len(m.fields)
		}
		if field.Dimension != "" {
			if _, exists := m.byDimension[field.Dimension]; exists {
				return nil, errors.NewCode(errors.InvalidInput, "duplicate dimension binding").WithContext("dimension", string(field.Dimension))
			}
			t := field.Type
			if t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			switch t.Kind() {
			case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			default:
				return nil, errors.NewCode(errors.InvalidInput, "dimension must bind a scalar field").WithContext("field", field.Name)
			}
			m.byDimension[field.Dimension] = len(m.fields)
		}
		m.byName[field.Name] = len(m.fields)
		m.fields = append(m.fields, field)
	}
	return m, nil
}

// Type 返回实体结构体类型。
func (m *Metadata) Type() reflect.Type { return m.typ }

// Fields 返回字段快照。
func (m *Metadata) Fields() []Field {
	return append([]Field(nil), m.fields...)
}

// Field 按字段名查找。
func (m *Metadata) Field(name string) (Field, bool) {
	idx, ok := m.byName[name]
	if !ok {
		return Field{}, false
	}
	return m.fields[idx], true
}

// DimensionField 按维度查找字段。
func (m *Metadata) DimensionField(d Dimension) (Field, bool) {
	idx, ok := m.byDimension[d]
	if !ok {
		return Field{}, false
	}
	return m.fields[idx], true
}

// IDField 返回实体标识字段。
func (m *Metadata) IDField() (Field, bool) {
	if m.id < 0 {
		return Field{}, false
	}
	return m.fields[m.id], true
}

// Resolve 将不同引用解析为同一实体字段身份。
func (m *Metadata) Resolve(ref Ref) (Field, error) {
	if err := ref.Validate(); err != nil {
		return Field{}, err
	}
	var f Field
	var ok bool
	switch ref.Kind() {
	case RefID:
		f, ok = m.IDField()
	case RefField:
		f, ok = m.Field(ref.Name())
	case RefDimension:
		f, ok = m.DimensionField(Dimension(ref.Name()))
	}
	if !ok {
		return Field{}, errors.NewCode(errors.InvalidInput, "reference has no field binding").WithContext("reference", ref.String())
	}
	return f, nil
}

// Read 读取字段事实，指针的 nil 保留为 NULL。
func (m *Metadata) Read(entity any, ref Ref) (any, error) {
	f, err := m.Resolve(ref)
	if err != nil {
		return nil, err
	}
	return f.Read(entity)
}

// Source 将实体与类型描述绑定为规约值来源。
func (m *Metadata) Source(entity any) EntitySource {
	return EntitySource{metadata: m, entity: entity}
}

// EntitySource 提供字段描述和实体当前值。
type EntitySource struct {
	metadata *Metadata
	entity   any
}

// Read 解析字段并读取其独立标量值。
func (s EntitySource) Read(ref Ref) (Field, any, error) {
	f, err := s.metadata.Resolve(ref)
	if err != nil {
		return Field{}, nil, err
	}
	v, err := f.Read(s.entity)
	return f, v, err
}

// Read 返回独立的标量值或 NULL；无法读取时明确报错。
func (f Field) Read(entity any) (any, error) {
	v, ok := f.Value(entity)
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "field cannot be read").WithContext("field", f.Name)
	}
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Func:
		if v.IsNil() {
			return nil, nil
		}
	}
	return v.Interface(), nil
}

// ManagedFields 返回由框架托管、且声明了 managed:"true" 的字段。
func (m *Metadata) ManagedFields() []Field {
	var out []Field
	for _, f := range m.fields {
		if f.Managed {
			out = append(out, f)
		}
	}
	return out
}

// Value 读取实体上的字段值。entity 可以是结构体或其指针。
func (f Field) Value(entity any) (reflect.Value, bool) {
	v := reflect.ValueOf(entity)
	if !v.IsValid() {
		return reflect.Value{}, false
	}
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	for _, idx := range f.index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct || idx < 0 || idx >= v.NumField() {
			return reflect.Value{}, false
		}
		v = v.Field(idx)
	}
	return v, true
}

// IsZero 报告字段是否为零值。指针字段的零值是 nil。
func (f Field) IsZero(entity any) (bool, error) {
	v, ok := f.Value(entity)
	if !ok {
		return false, errors.NewCode(errors.InvalidInput, "field cannot be read").
			WithContext("field", f.Name)
	}
	return v.IsZero(), nil
}

// IsNull 报告字段是否为 NULL。内存实现中只有可空类型（指针、接口、
// map、slice、func）的 nil 视为 NULL；非可空类型恒为 false。
func (f Field) IsNull(entity any) (bool, error) {
	v, ok := f.Value(entity)
	if !ok {
		return false, errors.NewCode(errors.InvalidInput, "field cannot be read").
			WithContext("field", f.Name)
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return v.IsNil(), nil
	default:
		return false, nil
	}
}

// Convert 把规约或赋值中的值转换为字段类型。输入只允许 string、整数与 bool。
func (f Field) Convert(v any) (reflect.Value, error) {
	return convertTo(f.Type, v, f.Name)
}
