package basic

import (
	"strconv"
	"strings"
)

// Minimal SELECT query builder（可选）
type SelectBuilder struct {
	cols   []string
	table  string
	where  []string
	args   []any
	order  string
	limit  int
	offset int
}

// isSafeIdentifier 判断简单标识符是否合法。
//
// 允许形式：
//   - 单一标识符：foo, bar_1
//   - 带点限定名：table.column
//
// 规则（按段）：
//   - 每段不能为空；
//   - 首字符必须是字母或下划线 [A-Za-z_]；
//   - 后续字符必须是字母、数字或下划线 [A-Za-z0-9_]。
func isSafeIdentifier(name string) bool {
	if name == "" {
		return false
	}
	parts := strings.Split(name, ".")
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			ch := part[i]
			if i == 0 {
				if !((ch >= 'a' && ch <= 'z') ||
					(ch >= 'A' && ch <= 'Z') ||
					ch == '_') {
					return false
				}
			} else {
				if !((ch >= 'a' && ch <= 'z') ||
					(ch >= 'A' && ch <= 'Z') ||
					(ch >= '0' && ch <= '9') ||
					ch == '_') {
					return false
				}
			}
		}
	}
	return true
}

func NewSelect() *SelectBuilder { return &SelectBuilder{cols: []string{"*"}} }

func (b *SelectBuilder) Select(columns ...string) *SelectBuilder {
	if len(columns) > 0 {
		safe := make([]string, 0, len(columns))
		for _, c := range columns {
			if c == "*" || isSafeIdentifier(c) {
				safe = append(safe, c)
			} else {
				panic("SelectBuilder: unsafe column name " + c)
			}
		}
		b.cols = safe
	}
	return b
}
func (b *SelectBuilder) From(table string) *SelectBuilder {
	if !isSafeIdentifier(table) {
		panic("SelectBuilder: unsafe table name " + table)
	}
	b.table = table
	return b
}
func (b *SelectBuilder) Where(cond string, args ...any) *SelectBuilder {
	if cond != "" {
		b.where = append(b.where, cond)
		b.args = append(b.args, args...)
	}
	return b
}
func (b *SelectBuilder) OrderBy(col string, desc bool) *SelectBuilder {
	if col != "" {
		if !isSafeIdentifier(col) {
			panic("SelectBuilder: unsafe order column " + col)
		}
		b.order = col
		if desc {
			b.order += " DESC"
		}
	}
	return b
}

// Limit 设置结果集最大行数。
//
// 约定：
//   - n > 0：生成 `LIMIT n` 子句；
//   - n == 0：不生成 LIMIT 子句（等价于“不限制”）；
//   - n < 0：视为编程错误，直接 panic 以便尽早暴露问题。
func (b *SelectBuilder) Limit(n int) *SelectBuilder {
	if n < 0 {
		panic("SelectBuilder: limit cannot be negative")
	}
	b.limit = n
	return b
}

// Offset 设置结果集偏移量。
//
// 约定：
//   - n > 0：生成 `OFFSET n` 子句；
//   - n == 0：不生成 OFFSET 子句（从第 0 行开始）；
//   - n < 0：视为编程错误，直接 panic。
func (b *SelectBuilder) Offset(n int) *SelectBuilder {
	if n < 0 {
		panic("SelectBuilder: offset cannot be negative")
	}
	b.offset = n
	return b
}

func (b *SelectBuilder) Build() (string, []any) {
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(b.cols, ","))
	sb.WriteString(" FROM ")
	sb.WriteString(b.table)
	if len(b.where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.where, " AND "))
	}
	if b.order != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(b.order)
	}
	if b.limit > 0 {
		sb.WriteString(" LIMIT ")
		sb.WriteString(strconv.Itoa(b.limit))
	}
	if b.offset > 0 {
		sb.WriteString(" OFFSET ")
		sb.WriteString(strconv.Itoa(b.offset))
	}
	return sb.String(), b.args
}
