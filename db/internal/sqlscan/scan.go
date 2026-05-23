// Package sqlscan 提供数据库包内部共享的轻量 SQL 词法扫描工具。
package sqlscan

import "strings"

// ScanSingleQuoted 扫描 SQL 单引号字面量，支持标准 SQL 的连续单引号转义。
func ScanSingleQuoted(text string, start int) int {
	for i := start + 1; i < len(text); i++ {
		if text[i] != '\'' {
			continue
		}
		if i+1 < len(text) && text[i+1] == '\'' {
			i++
			continue
		}
		return i + 1
	}
	return len(text)
}

// ScanBackslashSingleQuoted 扫描 PostgreSQL E'...' 这类带反斜杠转义的字面量。
func ScanBackslashSingleQuoted(text string, quote int) int {
	for i := quote + 1; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++
			continue
		}
		if text[i] != '\'' {
			continue
		}
		if i+1 < len(text) && text[i+1] == '\'' {
			i++
			continue
		}
		return i + 1
	}
	return len(text)
}

// ScanQuoted 扫描使用指定引号包裹的 SQL 标识符或字面量。
func ScanQuoted(text string, start int, quote byte) int {
	for i := start + 1; i < len(text); i++ {
		if text[i] != quote {
			continue
		}
		if i+1 < len(text) && text[i+1] == quote {
			i++
			continue
		}
		return i + 1
	}
	return len(text)
}

// ScanDollarQuoted 扫描 PostgreSQL dollar-quoted block。
func ScanDollarQuoted(text string, start int) (int, bool) {
	delimiter, ok := ReadDollarQuoteDelimiter(text[start:])
	if !ok {
		return start, false
	}
	end := strings.Index(text[start+len(delimiter):], delimiter)
	if end < 0 {
		return len(text), true
	}
	return start + len(delimiter) + end + len(delimiter), true
}

// ReadDollarQuoteDelimiter 读取 PostgreSQL dollar-quote 分隔符。
func ReadDollarQuoteDelimiter(text string) (string, bool) {
	if text == "" || text[0] != '$' {
		return "", false
	}
	for i := 1; i < len(text); i++ {
		ch := text[i]
		if ch == '$' {
			return text[:i+1], true
		}
		if i == 1 && !IsIdentStart(ch) {
			return "", false
		}
		if i > 1 && !IsIdentPart(ch) {
			return "", false
		}
	}
	return "", false
}

// ScanLineComment 扫描 -- 行注释，返回注释后的下一个位置。
func ScanLineComment(text string, start int) int {
	for i := start; i < len(text); i++ {
		if text[i] == '\n' {
			return i + 1
		}
	}
	return len(text)
}

// ScanNestedBlockComment 扫描支持嵌套的 /* ... */ 块注释。
func ScanNestedBlockComment(text string, start int) int {
	depth := 0
	for i := start; i < len(text); i++ {
		if text[i] == '/' && i+1 < len(text) && text[i+1] == '*' {
			i++
			depth++
			continue
		}
		if text[i] == '*' && i+1 < len(text) && text[i+1] == '/' {
			i++
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(text)
}

// IsSpace 判断字符是否为 SQL 常见空白。
func IsSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

// IsIdentStart 判断字符是否可作为 SQL 简单标识符开头。
func IsIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

// IsIdentPart 判断字符是否可作为 SQL 简单标识符组成部分。
func IsIdentPart(ch byte) bool {
	return IsIdentStart(ch) || (ch >= '0' && ch <= '9')
}
