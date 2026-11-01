package meta

import (
	"reflect"
	"strconv"
	"sync"

	"gochen/errors"
)

// Field 描述实体上的一个导出字段。
type Field struct {
	Name    string
	Type    reflect.Type
	Role    Role
	Managed bool

	index []int
}

// Metadata 是实体类型的字段元数据快照。
type Metadata struct {
	typ    reflect.Type
	fields []Field
	byName map[string]int
	byRole map[Role]int
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
		typ:    t,
		byName: make(map[string]int),
		byRole: make(map[Role]int),
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
		if raw := f.Tag.Get("role"); raw != "" {
			role, ok := ParseRole(raw)
			if !ok {
				return nil, errors.NewCode(errors.InvalidInput, "unknown field role").
					WithContext("field", f.Name).
					WithContext("role", raw)
			}
			field.Role = role
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
		if field.Role != "" {
			if _, exists := m.byRole[field.Role]; exists {
				return nil, errors.NewCode(errors.InvalidInput, "duplicate field role").
					WithContext("role", string(field.Role))
			}
			m.byRole[field.Role] = len(m.fields)
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

// RoleField 按角色查找字段。
func (m *Metadata) RoleField(role Role) (Field, bool) {
	idx, ok := m.byRole[role]
	if !ok {
		return Field{}, false
	}
	return m.fields[idx], true
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
