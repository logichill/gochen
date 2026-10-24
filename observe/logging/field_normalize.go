package logging

import (
	"fmt"
	"reflect"
	"strings"
)

// redactedValue 敏感字段脱敏后的统一掩码值。
const redactedValue = "[REDACTED]"

// sensitiveFieldKeys 需要强制脱敏的高敏字段键集合（小写匹配）。
//
// 为什么在日志管线内建词表：字段值一旦落盘即难以回收，而调用方很难保证
// 每个调用点都不误传凭证；在统一出口处按键名掩码是最后一道防线。
// 覆盖：鉴权凭证（authorization/token/bearer）、密钥类（secret/password/
// api_key/apikey/access_key）、会话凭证（cookie/set-cookie/session）、
// 以及常见变体（passwd/api-token/refresh_token 等）。
var sensitiveFieldKeys = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"set_cookie":    {},
	"set-cookie":    {},
	"password":      {},
	"passwd":        {},
	"secret":        {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"id_token":      {},
	"api_key":       {},
	"apikey":        {},
	"api-token":     {},
	"access_key":    {},
	"secret_key":    {},
	"private_key":   {},
	"session":       {},
	"session_id":    {},
	"bearer":        {},
	"credential":    {},
	"credentials":   {},
	"client_secret": {},
	"x-api-key":     {},
}

// isSensitiveFieldKey 判断字段键是否命中敏感词表。
//
// 匹配规则：先按完整键（小写、'-' 归一为 '_'）精确匹配；再按键尾段匹配
// （如 "user_password" / "req_header_x-api-key"），避免调用方以业务前缀
// 包装敏感值绕过脱敏。
func isSensitiveFieldKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	if normalized == "" {
		return false
	}
	if _, ok := sensitiveFieldKeys[normalized]; ok {
		return true
	}
	for _, segment := range strings.Split(normalized, "_") {
		if _, ok := sensitiveFieldKeys[segment]; ok {
			return true
		}
	}
	// 处理下划线复合词（如 access_token / client_secret）作为尾段出现。
	for suffix := range sensitiveFieldKeys {
		if strings.HasSuffix(normalized, "_"+suffix) {
			return true
		}
	}
	return false
}

type normalizedFields struct {
	component string
	event     string
	others    []Field
}

func isTypedNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// formatValue 把字段值转换为适合单行日志输出的文本。
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return escapeNewlines(val)
	case error:
		return escapeNewlines(val.Error())
	default:
		return escapeNewlines(fmt.Sprint(val))
	}
}

// escapeNewlines 把换行转义为文本，避免单条日志拆成多行。
func escapeNewlines(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r", "\\r")
	return strings.ReplaceAll(s, "\n", "\\n")
}

func normalizeFields(base []Field, fields ...Field) normalizedFields {
	allFields := append(append([]Field{}, base...), fields...)

	hasStack := false
	for _, f := range allFields {
		if f.Key == "stack" {
			hasStack = true
			break
		}
	}
	if !hasStack {
		for _, f := range allFields {
			if f.Key != "error" || f.Value == nil || isTypedNil(f.Value) {
				continue
			}
			e, ok := f.Value.(interface{ Details() map[string]any })
			if !ok {
				continue
			}
			details := e.Details()
			if details == nil {
				continue
			}
			stack, ok := details["stack"].(string)
			if !ok || strings.TrimSpace(stack) == "" {
				continue
			}
			allFields = append(allFields, Field{Key: "stack", Value: stack})
			break
		}
	}

	result := normalizedFields{others: make([]Field, 0, len(allFields))}
	for _, f := range allFields {
		switch f.Key {
		case "component":
			result.component = formatValue(f.Value)
		case "event":
			result.event = formatValue(f.Value)
		default:
			if isSensitiveFieldKey(f.Key) {
				result.others = append(result.others, Field{Key: f.Key, Value: redactedValue})
				continue
			}
			result.others = append(result.others, f)
		}
	}
	return result
}
