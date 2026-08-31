// Package action 提供 L1 安全能力：判断当前主体能否执行某个动作（权限码）。
//
// 设计边界：
//   - 本包只回答"能不能做这个动作"，不认识租户、数据范围、资源与策略快照；
//   - 严禁依赖 auth/scoped、auth/governance；
//   - 主体身份由各 Transport 的 AuthN 中间件写入 contextx，本包只消费 ctx。
package action

import (
	"regexp"
	"strings"
)

// maxCodeLength 限制权限码长度，避免异常输入放大正则回溯开销。
const maxCodeLength = 128

// codePattern 校验三段式权限码：`resource:type:action`。
//
// 资源段（首段）允许 `.` 与 `-`，用于表达 `ems.costing.price`、`statistics-owner`
// 这类命名空间化或面向导航的资源；type/action 均为稳定的短 token。
// `*` 仅允许作为某一段的整体通配，不与其他字符混用。
var codePattern = regexp.MustCompile(`^(\*|[A-Za-z0-9_.-]+):(\*|[A-Za-z0-9_]+):(\*|[A-Za-z0-9_]+)$`)

// IsValidCode 判断权限码是否满足三段式命名约束。
func IsValidCode(code string) bool {
	if len(code) == 0 || len(code) > maxCodeLength {
		return false
	}
	return codePattern.MatchString(code)
}

// PatternMatches 判断 pattern 是否命中 code。
//
// 约定：
// - 仅支持整段通配 `*`；
// - `pattern` 与 `code` 都使用三段式权限码；
// - 比较时大小写不敏感。
func PatternMatches(pattern string, code string) bool {
	patternSegments, ok := segments(pattern)
	if !ok {
		return false
	}
	codeSegments, ok := segments(code)
	if !ok {
		return false
	}

	for i := range patternSegments {
		if patternSegments[i] == "*" {
			continue
		}
		if patternSegments[i] != codeSegments[i] {
			return false
		}
	}
	return true
}

func segments(code string) ([3]string, bool) {
	var out [3]string
	normalized := strings.ToLower(strings.TrimSpace(code))
	if normalized == "" {
		return out, false
	}
	if !IsValidCode(normalized) {
		return out, false
	}
	parts := strings.Split(normalized, ":")
	if len(parts) != len(out) {
		return out, false
	}
	copy(out[:], parts)
	return out, true
}
