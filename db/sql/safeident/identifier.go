package safeident

import "strings"

// IsSafeIdentifier 判断标识符是否为“安全的数据库标识符”。
func IsSafeIdentifier(name string) bool {
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
