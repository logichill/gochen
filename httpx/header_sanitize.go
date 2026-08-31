package httpx

import "strings"

func SanitizeIdentifierFromHeader(raw string, maxLen int) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if maxLen <= 0 {
		maxLen = 128
	}
	if len(s) > maxLen {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == '.' || c == ':':
		default:
			return ""
		}
	}
	return s
}
