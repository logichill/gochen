package testkit

import (
	"reflect"
	"strings"

	"gochen/codec/casecodec"
)

// memoryFieldValueByIndex 按列名解析字段索引并逐级取值；解析路径穿过
// nil 指针或非结构体层级时返回 false（对齐 gochen-runtime 侧 resolveField
// 的 nil 守卫），避免 reflect.Value.FieldByIndex 在穿透 nil 指针时 panic。
func memoryFieldValueByIndex(value reflect.Value, column string) (reflect.Value, bool) {
	index, ok := memoryFieldByColumn(value.Type(), column)
	if !ok {
		return reflect.Value{}, false
	}
	for _, i := range index {
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				return reflect.Value{}, false
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		value = value.Field(i)
	}
	return value, true
}

// memoryFieldByColumn 在实体类型上按 SQL 侧列名约定查找字段索引。
func memoryFieldByColumn(typ reflect.Type, target string) ([]int, bool) {
	target = strings.TrimSpace(target)
	if typ == nil || target == "" {
		return nil, false
	}
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, false
	}
	return memoryWalkFieldByColumn(typ, target, nil, map[reflect.Type]int{})
}

// memoryWalkFieldByColumn 递归查找字段；visiting 记录当前递归栈上的类型，
// 自引用/互引用实体（如树形结构的 *Parent/*Child 字段）重复进入同一类型时
// 直接跳过，避免无限递归导致栈溢出（与 gochen-runtime
// db/orm/repo/datascope_schema.go 的 findFieldByColumn 一致）。
func memoryWalkFieldByColumn(t reflect.Type, target string, prefix []int, visiting map[reflect.Type]int) ([]int, bool) {
	if visiting[t] > 0 {
		return nil, false
	}
	visiting[t]++
	defer func() { visiting[t]-- }()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		if memoryColumnName(field) == target {
			return index, true
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() != reflect.Struct || fieldType.PkgPath() == "time" {
			continue
		}
		if matched, ok := memoryWalkFieldByColumn(fieldType, target, index, visiting); ok {
			return matched, true
		}
	}
	return nil, false
}

// memoryColumnName 与 runtime 侧 normalizeColumnName 一致：
// gorm column tag > db tag > json tag > snake_case 字段名。
func memoryColumnName(field reflect.StructField) string {
	if tag := strings.TrimSpace(field.Tag.Get("gorm")); tag != "" {
		for _, part := range strings.Split(tag, ";") {
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
	return memoryToSnakeCase(field.Name)
}

// memoryToSnakeCase 把 Go 字段名渲染为 snake_case 列名。
//
// 委托 gochen/codec/casecodec（与 runtime db/orm/repo/datascope_schema.go
// 的 toSnakeCase 同源同参），保证替身与生产列名口径一致——自制实现会把
// 缩写词渲染为 owner_i_d/managed_scope_i_d，与生产 owner_id 分叉（审计 P2#25）。
// 渲染不会失败，故忽略 error 以保持单返回值形态。
func memoryToSnakeCase(name string) string {
	encoded, _ := casecodec.Convert(casecodec.Snake, name)
	return encoded
}
