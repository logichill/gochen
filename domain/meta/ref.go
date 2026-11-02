package meta

import (
	"go/token"

	"gochen/errors"
)

// RefKind 区分实体标识、字段与维度引用。
type RefKind uint8

const (
	// RefID 引用实体标识。
	RefID RefKind = iota + 1
	// RefField 引用导出字段。
	RefField
	// RefDimension 引用命名维度。
	RefDimension
)

// Ref 是可比较的类型化字段引用；零值无效。
type Ref struct {
	kind RefKind
	name string
}

// ID 引用实体标识。
func ID() Ref { return Ref{kind: RefID} }

// ByField 引用字段；名称由 Validate 与实体描述验证。
func ByField(name string) Ref { return Ref{kind: RefField, name: name} }

// ByDimension 引用项目维度。
func ByDimension(d Dimension) Ref { return Ref{kind: RefDimension, name: string(d)} }

// Kind 返回引用种类。
func (r Ref) Kind() RefKind { return r.kind }

// Name 返回字段名或维度名。
func (r Ref) Name() string { return r.name }

// Validate 验证引用语法。
func (r Ref) Validate() error {
	switch r.kind {
	case RefID:
		if r.name == "" {
			return nil
		}
	case RefField:
		if token.IsIdentifier(r.name) {
			return nil
		}
	case RefDimension:
		_, err := ParseDimension(r.name)
		return err
	}
	return errors.NewCode(errors.InvalidInput, "invalid field reference").WithContext("reference", r.String())
}

// String 返回含种类的稳定诊断表示。
func (r Ref) String() string {
	switch r.kind {
	case RefID:
		return "id"
	case RefField:
		return "field:" + r.name
	case RefDimension:
		return "dim:" + r.name
	default:
		return "<invalid>"
	}
}
