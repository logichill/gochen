package spec

import (
	"reflect"
	"strings"

	"gochen/domain/meta"
	"gochen/errors"
)

// NamedMatcher 是内存解释器使用的命名条件实现，仅供读路径与契约测试。
type NamedMatcher func(entity any, args []any) (bool, error)

// InterpreterOption 配置内存解释器。
type InterpreterOption func(*Interpreter)

// WithNamed 注册一个命名条件。
func WithNamed(name string, matcher NamedMatcher) InterpreterOption {
	return func(i *Interpreter) {
		if i.named == nil {
			i.named = make(map[string]NamedMatcher)
		}
		i.named[strings.TrimSpace(name)] = matcher
	}
}

// Interpreter 是 Spec 的内存解释器。
type Interpreter struct {
	named map[string]NamedMatcher
}

// NewInterpreter 创建内存解释器。
func NewInterpreter(opts ...InterpreterOption) *Interpreter {
	i := &Interpreter{named: make(map[string]NamedMatcher)}
	for _, opt := range opts {
		if opt != nil {
			opt(i)
		}
	}
	return i
}

// Match 判断实体是否满足规约。非法规约、未知字段与类型转换失败都返回错误，
// 不当作不匹配。
func (i *Interpreter) Match(s Spec, entity any) (bool, error) {
	if s.err != nil {
		return false, errors.Wrap(s.err, errors.Internal, "invalid spec")
	}
	switch s.kind {
	case specNone:
		return false, nil
	case specAll:
		return true, nil
	case specAnd:
		for _, child := range s.children {
			ok, err := i.Match(child, entity)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case specOr:
		for _, child := range s.children {
			ok, err := i.Match(child, entity)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case specNamed:
		return i.matchNamed(s, entity)
	case specEq, specIn, specIsNull:
		return i.matchField(s, entity)
	default:
		return false, errors.NewCode(errors.Internal, "unknown spec kind")
	}
}

func (i *Interpreter) matchNamed(s Spec, entity any) (bool, error) {
	matcher, ok := i.named[s.name]
	if !ok || matcher == nil {
		return false, errors.NewCode(errors.Internal, "named condition is not registered").
			WithContext("name", s.name)
	}
	ok, err := matcher(entity, s.args)
	if err != nil {
		return false, errors.Wrap(err, errors.Internal, "named condition failed").
			WithContext("name", s.name)
	}
	return ok, nil
}

func (i *Interpreter) matchField(s Spec, entity any) (bool, error) {
	md, err := meta.DescribeValue(entity)
	if err != nil {
		return false, errors.Wrap(err, errors.Internal, "entity metadata unavailable")
	}
	field, err := resolveField(md, s.field)
	if err != nil {
		return false, err
	}

	if s.kind == specIsNull {
		null, err := field.IsNull(entity)
		if err != nil {
			return false, errors.Wrap(err, errors.Internal, "field read failed")
		}
		return null, nil
	}

	current, ok := field.Value(entity)
	if !ok {
		return false, errors.NewCode(errors.Internal, "field cannot be read").
			WithContext("field", field.Name)
	}
	if isNullValue(current) {
		return false, nil
	}

	if s.kind == specEq {
		return matchEq(field, current, s.value)
	}
	for _, raw := range s.values {
		ok, err := matchEq(field, current, raw)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func matchEq(field meta.Field, current reflect.Value, raw any) (bool, error) {
	converted, err := field.Convert(raw)
	if err != nil {
		return false, errors.Wrap(err, errors.Internal, "spec value conversion failed").
			WithContext("field", field.Name)
	}
	return reflect.DeepEqual(current.Interface(), converted.Interface()), nil
}

func resolveField(md *meta.Metadata, name string) (meta.Field, error) {
	if strings.HasPrefix(name, "@") {
		field, ok := md.RoleField(meta.Role(name))
		if !ok {
			return meta.Field{}, errors.NewCode(errors.Internal, "role is not mapped to a field").
				WithContext("role", name)
		}
		return field, nil
	}
	field, ok := md.Field(name)
	if !ok {
		return meta.Field{}, errors.NewCode(errors.Internal, "unknown spec field").
			WithContext("field", name)
	}
	return field, nil
}

func isNullValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}
