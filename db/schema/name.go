package schema

import "strings"

// ParseTableName 把 schema.table 或 table 拆成显式 schema 与表名。
func ParseTableName(name string) (schemaName string, tableName string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	parts := strings.Split(name, ".")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", name
}
