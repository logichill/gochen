package querymatch

import (
	"strings"

	"gochen/db/query"
	"gochen/errors"
)

// Text 表示需要保留 operator 语义的字符串过滤条件。
type Text struct {
	Value string
	Op    query.FilterOp
}

// LikeText 创建一个模糊匹配文本过滤条件。
func LikeText(value string) Text {
	return Text{Value: value, Op: query.FilterOpLike}
}

// IsZero 判断是否为空过滤条件。
func (t Text) IsZero() bool {
	return strings.TrimSpace(t.Value) == ""
}

// DecodeQueryExprs 实现 querybind.IDecoder。
func (t *Text) DecodeQueryExprs(exprs []query.QueryExpr) error {
	if len(exprs) != 1 {
		return errors.NewCode(errors.InvalidInput, "text matcher expects exactly one query expression")
	}
	if !supportsTextOp(exprs[0].Op) {
		return errors.NewCode(errors.InvalidInput, "text matcher only supports eq/like operator").
			WithContext("operator", string(exprs[0].Op))
	}
	t.Value = exprs[0].Value.String
	t.Op = exprs[0].Op
	return nil
}

// Match 按原始大小写做精确/模糊匹配。
func (t Text) Match(candidate string) bool {
	if t.IsZero() {
		return true
	}
	switch effectiveTextOp(t.Op) {
	case query.FilterOpEq:
		return candidate == t.Value
	case query.FilterOpLike:
		return strings.Contains(candidate, t.Value)
	default:
		return false
	}
}

// MatchFold 按大小写不敏感语义做精确/模糊匹配。
func (t Text) MatchFold(candidate string) bool {
	if t.IsZero() {
		return true
	}
	value := strings.ToLower(t.Value)
	candidate = strings.ToLower(candidate)
	switch effectiveTextOp(t.Op) {
	case query.FilterOpEq:
		return candidate == value
	case query.FilterOpLike:
		return strings.Contains(candidate, value)
	default:
		return false
	}
}

// Prefix 表示保留 eq/like 语义的前缀过滤条件。
type Prefix struct {
	Value string
	Op    query.FilterOp
}

// IsZero 判断是否为空过滤条件。
func (p Prefix) IsZero() bool {
	return strings.TrimSpace(p.Value) == ""
}

// DecodeQueryExprs 实现 querybind.IDecoder。
func (p *Prefix) DecodeQueryExprs(exprs []query.QueryExpr) error {
	if len(exprs) != 1 {
		return errors.NewCode(errors.InvalidInput, "prefix matcher expects exactly one query expression")
	}
	if !supportsTextOp(exprs[0].Op) {
		return errors.NewCode(errors.InvalidInput, "prefix matcher only supports eq/like operator").
			WithContext("operator", string(exprs[0].Op))
	}
	p.Value = exprs[0].Value.String
	p.Op = exprs[0].Op
	return nil
}

// Match 对字符串执行精确/前缀匹配。
func (p Prefix) Match(candidate string) bool {
	if p.IsZero() {
		return true
	}
	switch effectiveTextOp(p.Op) {
	case query.FilterOpEq:
		return candidate == p.Value
	case query.FilterOpLike:
		return strings.HasPrefix(candidate, p.Value)
	default:
		return false
	}
}

func effectiveTextOp(op query.FilterOp) query.FilterOp {
	if op == "" {
		return query.FilterOpEq
	}
	return op
}

func supportsTextOp(op query.FilterOp) bool {
	switch effectiveTextOp(op) {
	case query.FilterOpEq, query.FilterOpLike:
		return true
	default:
		return false
	}
}
