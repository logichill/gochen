package spec

import "gochen/domain/meta"

// Kind 是规约节点的种类，供 Core 之外的解释器（例如 Runtime 的 SQL 解释器）
// 读取规约结构。
type Kind string

const (
	KindNone   Kind = "none"
	KindAll    Kind = "all"
	KindEq     Kind = "eq"
	KindIn     Kind = "in"
	KindIsNull Kind = "isnull"
	KindAnd    Kind = "and"
	KindOr     Kind = "or"
	KindNamed  Kind = "named"
)

// Kind 返回规约节点种类。非法规约返回空值。
func (s Spec) Kind() Kind {
	if s.err != nil {
		return ""
	}
	switch s.kind {
	case specNone:
		return KindNone
	case specAll:
		return KindAll
	case specEq:
		return KindEq
	case specIn:
		return KindIn
	case specIsNull:
		return KindIsNull
	case specAnd:
		return KindAnd
	case specOr:
		return KindOr
	case specNamed:
		return KindNamed
	default:
		return ""
	}
}

// Ref 返回比较节点的类型化引用。
func (s Spec) Ref() meta.Ref { return s.field }

// Value 返回 Eq 节点的值。
func (s Spec) Value() any { return s.value }

// Values 返回 In 节点的值集合。
func (s Spec) Values() []any { return append([]any(nil), s.values...) }

// Name 返回 Named 节点的名称。
func (s Spec) Name() string { return s.name }

// Args 返回 Named 节点的参数。
func (s Spec) Args() []any { return append([]any(nil), s.args...) }

// Children 返回 And / Or 节点的子规约。
func (s Spec) Children() []Spec { return append([]Spec(nil), s.children...) }
