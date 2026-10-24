package query

import (
	"strings"
)

// QueryExpr 表示一个字段上的单条查询表达式。
type QueryExpr struct {
	Op     FilterOp
	Value  QueryValue
	Values []QueryValue
}

// QueryFilters 表示按字段归组后的查询表达式集合。
type QueryFilters map[string][]QueryExpr

func cloneQueryExpr(expr QueryExpr) QueryExpr {
	cloned := expr
	if len(expr.Values) > 0 {
		cloned.Values = append([]QueryValue(nil), expr.Values...)
	}
	return cloned
}

func cloneQueryExprs(exprs []QueryExpr) []QueryExpr {
	if len(exprs) == 0 {
		return nil
	}
	out := make([]QueryExpr, len(exprs))
	for i, expr := range exprs {
		out[i] = cloneQueryExpr(expr)
	}
	return out
}

// IsZero 判断是否为空过滤集合。
func (f QueryFilters) IsZero() bool {
	for _, exprs := range f {
		if len(exprs) > 0 {
			return false
		}
	}
	return true
}

// Has 判断指定字段是否存在过滤表达式。
func (f QueryFilters) Has(field string) bool {
	field = strings.TrimSpace(field)
	if field == "" {
		return false
	}
	return len(f[field]) > 0
}

func (f QueryFilters) Get(field string) []QueryExpr {
	field = strings.TrimSpace(field)
	if field == "" || len(f[field]) == 0 {
		return nil
	}
	return cloneQueryExprs(f[field])
}

func (f QueryFilters) First(field string) (QueryExpr, bool) {
	items := f.Get(field)
	if len(items) == 0 {
		return QueryExpr{}, false
	}
	return items[0], true
}

// Clone 返回深拷贝，避免外部后续修改影响调用方。
func (f QueryFilters) Clone() QueryFilters {
	if len(f) == 0 {
		return nil
	}
	out := make(QueryFilters, len(f))
	for field, exprs := range f {
		if len(exprs) == 0 {
			continue
		}
		out[field] = cloneQueryExprs(exprs)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Append 追加单条过滤表达式。
func (f QueryFilters) Append(field string, expr QueryExpr) QueryFilters {
	field = strings.TrimSpace(field)
	if field == "" {
		return f
	}
	if f == nil {
		f = make(QueryFilters)
	}
	f[field] = append(f[field], cloneQueryExpr(expr))
	return f
}

// Merge 追加另一组过滤表达式。
func (f QueryFilters) Merge(other QueryFilters) QueryFilters {
	if other.IsZero() {
		return f
	}
	if f == nil {
		f = make(QueryFilters, len(other))
	}
	for field, exprs := range other {
		if len(exprs) == 0 {
			continue
		}
		f[field] = append(f[field], cloneQueryExprs(exprs)...)
	}
	return f
}
