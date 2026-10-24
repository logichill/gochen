package query

import (
	"sort"
	"strings"
)

// QuerySchema 表示动态查询 DSL 的字段能力声明。
type QuerySchema struct {
	fields map[string]QueryField
}

// NewQuerySchema 创建查询字段 schema。
func NewQuerySchema(fields ...QueryField) *QuerySchema {
	schema := &QuerySchema{
		fields: make(map[string]QueryField, len(fields)),
	}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		field.Name = name
		if len(field.FilterOps) == 0 {
			field.FilterOps = defaultFilterOps(field.Type)
		} else {
			field.FilterOps = cloneFilterOps(field.FilterOps)
		}
		schema.fields[name] = field
	}
	if len(schema.fields) == 0 {
		return nil
	}
	return schema
}

func (s *QuerySchema) Field(name string) (QueryField, bool) {
	if s == nil {
		return QueryField{}, false
	}
	field, ok := s.fields[strings.TrimSpace(name)]
	return field, ok
}

// FilterableFieldNames 返回 schema 中声明的可过滤字段名。
func (s *QuerySchema) FilterableFieldNames() []string {
	return s.fieldNames(func(field QueryField) bool { return len(field.FilterOps) > 0 })
}

// SortableFieldNames 返回 schema 中声明的可排序字段名。
func (s *QuerySchema) SortableFieldNames() []string {
	return s.fieldNames(func(field QueryField) bool { return field.Sortable })
}

// SelectableFieldNames 返回 schema 中声明的可投影字段名。
func (s *QuerySchema) SelectableFieldNames() []string {
	return s.fieldNames(func(field QueryField) bool { return field.Selectable })
}

func (s *QuerySchema) fieldNames(accept func(QueryField) bool) []string {
	if s == nil || len(s.fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.fields))
	for name, field := range s.fields {
		if accept != nil && !accept(field) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}
