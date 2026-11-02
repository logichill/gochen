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

// IValueSource 提供规约所需的字段描述与当前标量值；NULL 使用 nil。
type IValueSource interface {
	Read(ref meta.Ref) (meta.Field, any, error)
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
	md, err := meta.DescribeValue(entity)
	if err != nil {
		return false, errors.Wrap(err, errors.Internal, "entity metadata unavailable")
	}
	return i.match(s, md.Source(entity), entity)
}

// MatchSource 使用实体或历史快照来源解释同一条件代数。
func (i *Interpreter) MatchSource(s Spec, source IValueSource) (bool, error) {
	return i.match(s, source, nil)
}

func (i *Interpreter) match(s Spec, source IValueSource, entity any) (bool, error) {
	if s.err != nil {
		return false, errors.Wrap(s.err, errors.Internal, "invalid spec")
	}
	switch s.kind {
	case specNone:
		return false, nil
	case specAll:
		return true, nil
	case specAnd:
		matched := true
		for _, child := range s.children {
			ok, err := i.match(child, source, entity)
			if err != nil {
				return false, err
			}
			matched = matched && ok
		}
		return matched, nil
	case specOr:
		matched := false
		for _, child := range s.children {
			ok, err := i.match(child, source, entity)
			if err != nil {
				return false, err
			}
			matched = matched || ok
		}
		return matched, nil
	case specNamed:
		return i.matchNamed(s, entity)
	case specEq, specIn, specIsNull:
		return i.matchField(s, source)
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

func (i *Interpreter) matchField(s Spec, source IValueSource) (bool, error) {
	field, value, err := source.Read(s.field)
	if err != nil {
		return false, errors.Wrap(err, errors.Internal, "field source read failed")
	}
	if s.kind == specIsNull {
		return value == nil, nil
	}
	if s.kind == specEq {
		return matchEq(field, value, s.value)
	}
	matched := false
	for _, raw := range s.values {
		ok, err := matchEq(field, value, raw)
		if err != nil {
			return false, err
		}
		matched = matched || ok
	}
	return matched, nil
}

func matchEq(field meta.Field, current any, raw any) (bool, error) {
	converted, err := field.Convert(raw)
	if err != nil {
		return false, errors.Wrap(err, errors.Internal, "spec value conversion failed").
			WithContext("field", field.Name)
	}
	if current == nil {
		return false, nil
	}
	for converted.Kind() == reflect.Ptr {
		converted = converted.Elem()
	}
	return reflect.DeepEqual(current, converted.Interface()), nil
}
