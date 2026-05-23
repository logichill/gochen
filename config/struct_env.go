package config

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// ApplyEnvOverridesByYAMLPath 按 yaml 路径推导环境变量名并覆盖结构体字段。
//
// 规则：
// - 默认使用 yaml 路径推导，如 `logging.sql.level -> LOGGING_SQL_LEVEL`
// - 字段可通过 `env:"FOO_BAR"` 显式指定环境变量名
// - 结构体字段可通过 `envprefix:"LOG"` 覆盖路径前缀
// - `env:"-"` 表示跳过该字段
// - 解析失败时保持原值，行为与现有 `getEnvXxx` 宽松覆盖保持一致
func ApplyEnvOverridesByYAMLPath(target any) {
	ApplyEnvOverridesByYAMLPathWithLookup(target, os.LookupEnv)
}

// ApplyEnvOverridesByYAMLPathWithLookup 允许调用方显式提供配置键查找源。
func ApplyEnvOverridesByYAMLPathWithLookup(target any, lookup func(string) (string, bool)) {
	applyEnvRecursive(reflect.ValueOf(target), nil, lookup)
}

func applyEnvRecursive(value reflect.Value, path []string, lookup func(string) (string, bool)) {
	if !value.IsValid() {
		return
	}

	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return
	}

	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		fieldValue := value.Field(index)
		fieldType := valueType.Field(index)
		if !fieldType.IsExported() {
			continue
		}

		envTag := strings.TrimSpace(fieldType.Tag.Get("env"))
		if envTag == "-" {
			continue
		}

		yamlName, ok := yamlFieldName(fieldType)
		if !ok {
			continue
		}

		nextPath := appendPath(path, yamlName)
		if prefix := strings.TrimSpace(fieldType.Tag.Get("envprefix")); prefix != "" {
			nextPath = splitEnvPrefix(prefix)
		}

		if isStructLikeField(fieldValue) {
			applyEnvRecursive(fieldValue, nextPath, lookup)
			continue
		}

		envKey := envTag
		if envKey == "" {
			envKey = pathToEnvKey(nextPath)
		}
		if envKey == "" {
			continue
		}

		raw, ok := lookup(envKey)
		if !ok {
			continue
		}
		assignEnvValue(fieldValue, raw)
	}
}

func isStructLikeField(value reflect.Value) bool {
	valueType := value.Type()
	if valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType.Kind() == reflect.Struct && valueType != reflect.TypeOf(time.Duration(0))
}

func yamlFieldName(field reflect.StructField) (string, bool) {
	tag := strings.TrimSpace(field.Tag.Get("yaml"))
	if tag == "-" {
		return "", false
	}
	if tag == "" {
		return strings.ToLower(field.Name), true
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return strings.ToLower(field.Name), true
	}
	return name, true
}

func appendPath(path []string, segment string) []string {
	if segment == "" {
		return append([]string(nil), path...)
	}
	next := make([]string, 0, len(path)+1)
	next = append(next, path...)
	next = append(next, segment)
	return next
}

func splitEnvPrefix(prefix string) []string {
	parts := strings.Split(prefix, "_")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, strings.ToLower(part))
		}
	}
	return result
}

func pathToEnvKey(path []string) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, 0, len(path))
	for _, segment := range path {
		normalized := strings.ToUpper(strings.ReplaceAll(segment, "-", "_"))
		normalized = strings.ReplaceAll(normalized, ".", "_")
		if normalized != "" {
			parts = append(parts, normalized)
		}
	}
	return strings.Join(parts, "_")
}

func assignEnvValue(field reflect.Value, raw string) {
	if !field.CanSet() {
		return
	}

	if field.Kind() == reflect.Pointer {
		assignPointerEnvValue(field, raw)
		return
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		if parsed, err := strconv.ParseBool(raw); err == nil {
			field.SetBool(parsed)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			if parsed, err := time.ParseDuration(raw); err == nil {
				field.SetInt(int64(parsed))
			}
			return
		}
		if parsed, err := strconv.ParseInt(raw, 10, field.Type().Bits()); err == nil {
			field.SetInt(parsed)
		}
	case reflect.Float32, reflect.Float64:
		if parsed, err := strconv.ParseFloat(raw, field.Type().Bits()); err == nil {
			field.SetFloat(parsed)
		}
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			field.Set(reflect.ValueOf(splitStringSlice(raw)))
		}
	}
}

func assignPointerEnvValue(field reflect.Value, raw string) {
	elemType := field.Type().Elem()
	switch elemType.Kind() {
	case reflect.Bool:
		if parsed, err := strconv.ParseBool(raw); err == nil {
			value := reflect.New(elemType)
			value.Elem().SetBool(parsed)
			field.Set(value)
		}
	case reflect.String:
		value := reflect.New(elemType)
		value.Elem().SetString(raw)
		field.Set(value)
	}
}

func splitStringSlice(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
