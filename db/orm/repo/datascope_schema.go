package repo

import (
	"reflect"
	"strings"

	"gochen/db/naming"
)

func inferDataScopeSchema[T any](columns accessColumns) dataScopeSchema {
	var zero T
	typ := reflect.TypeOf(zero)
	for typ != nil && typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	var schema dataScopeSchema
	walkStructFields(typ, nil, &schema, map[reflect.Type]int{})
	overrideSchemaField(typ, &schema.managedScope, columns.managedScope)
	overrideSchemaField(typ, &schema.ownerID, columns.ownerID)
	overrideSchemaField(typ, &schema.version, columns.version)
	return schema
}
func overrideSchemaField(typ reflect.Type, field *dataScopeField, column string) {
	column = strings.TrimSpace(column)
	if column == "" {
		return
	}
	*field = dataScopeField{column: column}
	if matched, ok := findFieldByColumn(typ, column, nil, map[reflect.Type]int{}); ok {
		field.index = matched.index
	}
}

func walkStructFields(typ reflect.Type, prefix []int, schema *dataScopeSchema, visiting map[reflect.Type]int) {
	if typ == nil {
		return
	}
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || isTimeType(typ) {
		return
	}
	if visiting[typ] > 0 {
		return
	}
	visiting[typ]++
	defer func() {
		visiting[typ]--
	}()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		column := normalizeColumnName(field)
		switch column {
		case "managed_scope_id":
			if schema.managedScope.column == "" {
				schema.managedScope = dataScopeField{column: column, index: index}
			}
		case "owner_id", "tenant_id":
			if schema.ownerID.column == "" {
				schema.ownerID = dataScopeField{column: column, index: index}
			}
		case "version":
			if schema.version.column == "" {
				schema.version = dataScopeField{column: column, index: index}
			}
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct && !isTimeType(fieldType) {
			walkStructFields(fieldType, index, schema, visiting)
		}
	}
}

func findFieldByColumn(typ reflect.Type, target string, prefix []int, visiting map[reflect.Type]int) (dataScopeField, bool) {
	target = strings.TrimSpace(target)
	if target == "" || typ == nil {
		return dataScopeField{}, false
	}
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || isTimeType(typ) {
		return dataScopeField{}, false
	}
	if visiting[typ] > 0 {
		return dataScopeField{}, false
	}
	visiting[typ]++
	defer func() {
		visiting[typ]--
	}()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		if normalizeColumnName(field) == target {
			return dataScopeField{column: target, index: index}, true
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() != reflect.Struct || isTimeType(fieldType) {
			continue
		}
		if matched, ok := findFieldByColumn(fieldType, target, index, visiting); ok {
			return matched, true
		}
	}
	return dataScopeField{}, false
}

func (s dataScopeSchema) hasConstraints() bool {
	return s.managedScope.column != "" || s.ownerID.column != "" || s.version.column != ""
}

func normalizeColumnName(field reflect.StructField) string {
	if column := columnFromTags(field); column != "" {
		return column
	}
	return toSnakeCase(field.Name)
}

func columnFromTags(field reflect.StructField) string {
	gormTag := strings.TrimSpace(field.Tag.Get("gorm"))
	if gormTag != "" {
		for _, part := range strings.Split(gormTag, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "column:") {
				return strings.TrimSpace(strings.TrimPrefix(part, "column:"))
			}
		}
	}
	if tag := strings.TrimSpace(field.Tag.Get("db")); tag != "" && tag != "-" {
		return tag
	}
	if tag := strings.TrimSpace(field.Tag.Get("json")); tag != "" && tag != "-" {
		return strings.TrimSpace(strings.Split(tag, ",")[0])
	}
	return ""
}
func isTimeType(t reflect.Type) bool {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t != nil && t.PkgPath() == "time" && t.Name() == "Time"
}

func toSnakeCase(value string) string {
	return naming.SnakeCase(value)
}
