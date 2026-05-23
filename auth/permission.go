package auth

import (
	"regexp"
	"strings"
)

// permissionCodePattern 校验三段式权限码：`type:resource:action`。
//
// 资源段额外允许 `.`，用于表达 `ecommerce.product` 这类命名空间化资源；
// type/action 不允许 `.`，以保持短 token 的语义稳定。
// `*` 仅允许作为某一段的整体通配，不与其他字符混用。
var permissionCodePattern = regexp.MustCompile(`^(\*|[A-Za-z0-9_]+):(\*|[A-Za-z0-9_.]+):(\*|[A-Za-z0-9_]+)$`)

// IsValidPermissionCode 判断权限码是否满足三段式命名约束。
func IsValidPermissionCode(permission string) bool {
	if len(permission) == 0 || len(permission) > 128 {
		return false
	}
	return permissionCodePattern.MatchString(permission)
}

// PermissionPatternMatches 判断 pattern 是否命中 permission。
//
// 约定：
// - 仅支持整段通配 `*`；
// - `pattern` 与 `permission` 都使用三段式权限码；
// - 比较时大小写不敏感。
func PermissionPatternMatches(pattern string, permission string) bool {
	patternSegments, ok := permissionSegments(pattern)
	if !ok {
		return false
	}
	permissionSegments, ok := permissionSegments(permission)
	if !ok {
		return false
	}

	for i := range patternSegments {
		if patternSegments[i] == "*" {
			continue
		}
		if patternSegments[i] != permissionSegments[i] {
			return false
		}
	}
	return true
}

func permissionSegments(permission string) ([3]string, bool) {
	var segments [3]string
	normalized := strings.ToLower(strings.TrimSpace(permission))
	if normalized == "" {
		return segments, false
	}
	if !IsValidPermissionCode(normalized) {
		return segments, false
	}
	parts := strings.Split(normalized, ":")
	if len(parts) != len(segments) {
		return segments, false
	}
	copy(segments[:], parts)
	return segments, true
}
