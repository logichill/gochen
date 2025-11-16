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
    args   []interface{}
    order  string
    limit  int
    offset int
}

func NewSelect() *SelectBuilder { return &SelectBuilder{cols: []string{"*"}} }

func (b *SelectBuilder) Select(columns ...string) *SelectBuilder {
    if len(columns) > 0 { b.cols = columns }
    return b
}
func (b *SelectBuilder) From(table string) *SelectBuilder { b.table = table; return b }
func (b *SelectBuilder) Where(cond string, args ...interface{}) *SelectBuilder {
    if cond != "" { b.where = append(b.where, cond); b.args = append(b.args, args...) }
    return b
}
func (b *SelectBuilder) OrderBy(col string, desc bool) *SelectBuilder {
    if col != "" { b.order = col; if desc { b.order += " DESC" } }
    return b
}
func (b *SelectBuilder) Limit(n int) *SelectBuilder  { b.limit = n; return b }
func (b *SelectBuilder) Offset(n int) *SelectBuilder { b.offset = n; return b }

func (b *SelectBuilder) Build() (string, []interface{}) {
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

