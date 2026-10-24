package query

import (
	"strings"

	"gochen/errors"
)

// Bound 表示区间边界。
type Bound[T any] struct {
	Value     T
	Inclusive bool
}

// Range 表示有序字段的范围查询。
type Range[T any] struct {
	Lower *Bound[T]
	Upper *Bound[T]
}

// IsZero 判断是否为空区间。
func (r Range[T]) IsZero() bool {
	return r.Lower == nil && r.Upper == nil
}

// LowerPtr 返回下界值指针；若不存在下界则返回 nil。
func (r Range[T]) LowerPtr() *T {
	if r.Lower == nil {
		return nil
	}
	value := r.Lower.Value
	return &value
}

// UpperPtr 返回上界值指针；若不存在上界则返回 nil。
func (r Range[T]) UpperPtr() *T {
	if r.Upper == nil {
		return nil
	}
	value := r.Upper.Value
	return &value
}

// LowerValue 返回下界值；若不存在下界则返回零值。
func (r Range[T]) LowerValue() T {
	var zero T
	if r.Lower == nil {
		return zero
	}
	return r.Lower.Value
}

// UpperValue 返回上界值；若不存在上界则返回零值。
func (r Range[T]) UpperValue() T {
	var zero T
	if r.Upper == nil {
		return zero
	}
	return r.Upper.Value
}

// DecodeQueryExprs 让 Range[T] 可以直接被 querybind 绑定。
func (r *Range[T]) DecodeQueryExprs(exprs []QueryExpr) error {
	// typed-nil 接管者防御：接收者任何字段访问都会 panic，直接报错。
	if r == nil {
		return errors.NewCode(errors.InvalidInput, "query range receiver is nil")
	}
	parsed, ok, err := ParseRange[T](exprs)
	if err != nil {
		return err
	}
	if !ok {
		*r = Range[T]{}
		return nil
	}
	*r = parsed
	return nil
}

// RangeFor 从 QueryFilters 中提取指定字段的范围表达式。
func RangeFor[T any](filters QueryFilters, field string) (Range[T], bool, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return Range[T]{}, false, errors.NewCode(errors.InvalidInput, "query range: field is empty")
	}
	return ParseRange[T](filters.Get(field))
}

// ParseRange 把同一字段上的 QueryExpr 解析为 Range[T]。
func ParseRange[T any](exprs []QueryExpr) (Range[T], bool, error) {
	parsed, ok, err := parseQueryValueRange(exprs, true)
	if err != nil || !ok {
		return Range[T]{}, ok, err
	}
	return convertRange[T](parsed)
}
