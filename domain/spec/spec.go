package spec

import (
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gochen/domain/meta"
	"gochen/errors"
)

// Spec 是领域层唯一的行级谓词类型。零值不匹配任何实体；非法构造携带错误，
// 任何解释器遇到它都返回该错误。
type Spec struct {
	kind     specKind
	field    meta.Ref
	value    any
	values   []any
	name     string
	args     []any
	children []Spec
	err      error
}

type specKind uint8

const (
	specNone specKind = iota
	specAll
	specEq
	specIn
	specIsNull
	specAnd
	specOr
	specNamed
)

const DefaultINLimit = 1000

var inLimit = struct {
	sync.RWMutex
	value      int
	configured bool
}{value: DefaultINLimit}

// ConfigureINLimit 设置进程级的 IN 上限，只允许配置一次。请求期不可变。
func ConfigureINLimit(limit int) error {
	if limit <= 0 {
		return errors.NewCode(errors.InvalidInput, "IN limit must be positive")
	}
	inLimit.Lock()
	defer inLimit.Unlock()
	if inLimit.configured {
		return errors.NewCode(errors.InvalidInput, "IN limit already configured")
	}
	inLimit.value = limit
	inLimit.configured = true
	return nil
}

// INLimit 返回当前进程的 IN 上限。
func INLimit() int {
	inLimit.RLock()
	defer inLimit.RUnlock()
	return inLimit.value
}

// All 返回匹配所有实体的规约。
func All() Spec { return Spec{kind: specAll} }

// Eq 返回 field 等于 v 的规约。v 为 nil 或类型不允许时得到非法规约。
func Eq(field meta.Ref, v any) Spec {
	f, err := normalizeField(field)
	if err != nil {
		return Spec{err: err}
	}
	value, err := normalizeValue(v)
	if err != nil {
		return Spec{err: err}
	}
	return Spec{kind: specEq, field: f, value: value}
}

// In 返回 field 属于给定值的规约。空集合等于零值；含 nil、超过 IN 上限或
// 出现不允许的值类型时得到非法规约。
func In(field meta.Ref, vs ...any) Spec {
	f, err := normalizeField(field)
	if err != nil {
		return Spec{err: err}
	}
	if len(vs) == 0 {
		return Spec{kind: specIn, field: f}
	}
	if limit := INLimit(); len(vs) > limit {
		return Spec{err: errors.NewCode(errors.InvalidInput, "IN exceeds configured limit").
			WithContext("field", f).
			WithContext("limit", limit).
			WithContext("size", len(vs))}
	}
	seen := make(map[string]struct{}, len(vs))
	values := make([]any, 0, len(vs))
	for _, raw := range vs {
		v, err := normalizeValue(raw)
		if err != nil {
			return Spec{err: err}
		}
		key := canonicalValue(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, v)
	}
	if len(values) == 1 {
		return Spec{kind: specEq, field: f, value: values[0]}
	}
	return Spec{kind: specIn, field: f, values: values}
}

// IsNull 返回 field 为 NULL 的规约。
func IsNull(field meta.Ref) Spec {
	f, err := normalizeField(field)
	if err != nil {
		return Spec{err: err}
	}
	return Spec{kind: specIsNull, field: f}
}

// And 组合多个规约。And() 等于 All()；任一子项为零值则结果为零值。
func And(ss ...Spec) Spec {
	for _, s := range ss {
		if s.err != nil {
			return Spec{err: s.err}
		}
	}
	out := make([]Spec, 0, len(ss))
	for _, s := range ss {
		switch s.kind {
		case specAll:
			continue
		case specNone:
			out = append(out, s)
		case specAnd:
			out = append(out, s.children...)
		default:
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return All()
	}
	if len(out) == 1 {
		return out[0]
	}
	return Spec{kind: specAnd, children: out}
}

// Or 组合多个规约。Or() 等于零值；任一子项为 All 则结果为 All。
func Or(ss ...Spec) Spec {
	for _, s := range ss {
		if s.err != nil {
			return Spec{err: s.err}
		}
	}
	out := make([]Spec, 0, len(ss))
	for _, s := range ss {
		switch s.kind {
		case specNone:
			continue
		case specAll:
			out = append(out, s)
		case specOr:
			out = append(out, s.children...)
		default:
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return Spec{}
	}
	if len(out) == 1 {
		return out[0]
	}
	return Spec{kind: specOr, children: out}
}

// Named 返回到仓储注册的命名条件。
func Named(name string, args ...any) Spec {
	name = strings.TrimSpace(name)
	if name == "" {
		return Spec{err: errors.NewCode(errors.InvalidInput, "named condition name cannot be empty")}
	}
	normalized := make([]any, 0, len(args))
	for _, raw := range args {
		v, err := normalizeValue(raw)
		if err != nil {
			return Spec{err: err}
		}
		normalized = append(normalized, v)
	}
	return Spec{kind: specNamed, name: name, args: normalized}
}

// Err 返回规约携带的构造错误。
func (s Spec) Err() error { return s.err }

// IsAll 报告规约是否匹配所有实体。
func (s Spec) IsAll() bool {
	if s.err != nil {
		return false
	}
	if s.kind == specAll {
		return true
	}
	if s.kind == specOr {
		for _, child := range s.children {
			if child.IsAll() {
				return true
			}
		}
	}
	if s.kind == specAnd {
		for _, child := range s.children {
			if !child.IsAll() {
				return false
			}
		}
		return true
	}
	return false
}

// IsNone 报告规约是否不匹配任何实体。
func (s Spec) IsNone() bool {
	if s.err != nil {
		return false
	}
	if s.kind == specNone || (s.kind == specIn && len(s.values) == 0) {
		return true
	}
	if s.kind == specAnd {
		for _, child := range s.children {
			if child.IsNone() {
				return true
			}
		}
	}
	if s.kind == specOr {
		for _, child := range s.children {
			if !child.IsNone() {
				return false
			}
		}
		return true
	}
	return false
}

// HasNamed 报告规约树中是否包含命名条件。
func (s Spec) HasNamed() bool {
	if s.err != nil {
		return false
	}
	if s.kind == specNamed {
		return true
	}
	for _, child := range s.children {
		if child.HasNamed() {
			return true
		}
	}
	return false
}

// References 报告规约树是否包含指定引用。
func (s Spec) References(ref meta.Ref) bool {
	if s.err != nil {
		return false
	}
	switch s.kind {
	case specEq, specIn, specIsNull:
		if s.field == ref {
			return true
		}
	case specAnd, specOr:
		for _, child := range s.children {
			if child.References(ref) {
				return true
			}
		}
	}
	return false
}

// NamedNames 返回规约树中命名条件的名称，按名称排序并去重。
func (s Spec) NamedNames() []string {
	if s.err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	collectNamed(s, seen)
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func collectNamed(s Spec, out map[string]struct{}) {
	switch s.kind {
	case specNamed:
		out[s.name] = struct{}{}
	case specAnd, specOr:
		for _, child := range s.children {
			collectNamed(child, out)
		}
	}
}

// Refs 返回规约树的类型化引用，按诊断表示排序并去重。
func (s Spec) Refs() []meta.Ref {
	if s.err != nil {
		return nil
	}
	seen := make(map[meta.Ref]struct{})
	collectFields(s, seen)
	out := make([]meta.Ref, 0, len(seen))
	for field := range seen {
		out = append(out, field)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func collectFields(s Spec, out map[meta.Ref]struct{}) {
	switch s.kind {
	case specEq, specIn, specIsNull:
		out[s.field] = struct{}{}
	case specAnd, specOr:
		for _, child := range s.children {
			collectFields(child, out)
		}
	}
}

// Canonical 返回规约的规范形式。非法规约返回 "<invalid>"。
func (s Spec) Canonical() string {
	if s.err != nil {
		return "<invalid>"
	}
	if s.IsAll() {
		return "all"
	}
	if s.IsNone() {
		return "none"
	}
	switch s.kind {
	case specNone:
		return "none"
	case specAll:
		return "all"
	case specEq:
		return "eq(" + s.field.String() + "," + canonicalValue(s.value) + ")"
	case specIn:
		parts := make([]string, 0, len(s.values))
		for _, v := range s.values {
			parts = append(parts, canonicalValue(v))
		}
		return "in(" + s.field.String() + "," + strings.Join(parts, ",") + ")"
	case specIsNull:
		return "isnull(" + s.field.String() + ")"
	case specNamed:
		parts := make([]string, 0, len(s.args)+1)
		parts = append(parts, s.name)
		for _, v := range s.args {
			parts = append(parts, canonicalValue(v))
		}
		return "named(" + strings.Join(parts, ",") + ")"
	case specAnd, specOr:
		parts := make([]string, 0, len(s.children))
		for _, child := range s.children {
			parts = append(parts, child.Canonical())
		}
		sort.Strings(parts)
		op := "and("
		if s.kind == specOr {
			op = "or("
		}
		return op + strings.Join(parts, ",") + ")"
	default:
		return "<invalid>"
	}
}

func normalizeField(field meta.Ref) (meta.Ref, error) {
	return field, field.Validate()
}

func normalizeValue(v any) (any, error) {
	if v == nil {
		return nil, errors.NewCode(errors.InvalidInput, "spec value cannot be nil; use IsNull")
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return nil, errors.NewCode(errors.InvalidInput, "unsigned value overflows int64")
		}
		return int64(u), nil
	case reflect.Bool:
		return rv.Bool(), nil
	default:
		return nil, errors.NewCode(errors.InvalidInput, "unsupported spec value type").
			WithContext("value_type", rv.Type().String())
	}
}

func canonicalValue(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return "<invalid>"
	}
}
