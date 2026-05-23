package migrate

import (
	"strings"

	"gochen/db/internal/sqlscan"
)

// SplitStatements 把常见 SQL migration 文件拆成按顺序执行的语句。
//
// 说明：该函数覆盖普通 DDL/DML、单/双引号、反引号、PostgreSQL dollar-quoted block、
// 行注释与块注释；复杂脚本可由后续方言 splitter 扩展。
func SplitStatements(sqlText string) []string {
	var (
		out               []string
		sb                strings.Builder
		inSingle          bool
		singleBackslash   bool
		inDouble          bool
		inBacktick        bool
		inLineComment     bool
		blockCommentDepth int
		dollarQuote       string
	)

	for i := 0; i < len(sqlText); i++ {
		ch := sqlText[i]
		next := byte(0)
		if i+1 < len(sqlText) {
			next = sqlText[i+1]
		}

		if dollarQuote != "" {
			if strings.HasPrefix(sqlText[i:], dollarQuote) {
				sb.WriteString(dollarQuote)
				i += len(dollarQuote) - 1
				dollarQuote = ""
				continue
			}
			sb.WriteByte(ch)
			continue
		}
		if inLineComment {
			sb.WriteByte(ch)
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if blockCommentDepth > 0 {
			sb.WriteByte(ch)
			if ch == '/' && next == '*' {
				sb.WriteByte(next)
				i++
				blockCommentDepth++
				continue
			}
			if ch == '*' && next == '/' {
				sb.WriteByte(next)
				i++
				blockCommentDepth--
			}
			continue
		}

		if !inSingle && !inDouble && !inBacktick {
			if ch == '-' && next == '-' {
				sb.WriteByte(ch)
				sb.WriteByte(next)
				i++
				inLineComment = true
				continue
			}
			if ch == '#' {
				sb.WriteByte(ch)
				inLineComment = true
				continue
			}
			if ch == '/' && next == '*' {
				sb.WriteByte(ch)
				sb.WriteByte(next)
				i++
				blockCommentDepth = 1
				continue
			}
			if ch == '$' {
				if delimiter, ok := sqlscan.ReadDollarQuoteDelimiter(sqlText[i:]); ok {
					sb.WriteString(delimiter)
					i += len(delimiter) - 1
					dollarQuote = delimiter
					continue
				}
			}
			if ch == ';' {
				appendStatement(&out, sb.String())
				sb.Reset()
				continue
			}
		}

		if inSingle && singleBackslash && ch == '\\' && next != 0 {
			sb.WriteByte(ch)
			sb.WriteByte(next)
			i++
			continue
		}
		if inSingle && ch == '\'' && next == '\'' {
			sb.WriteByte(ch)
			sb.WriteByte(next)
			i++
			continue
		}
		if inDouble && ch == '"' && next == '"' {
			sb.WriteByte(ch)
			sb.WriteByte(next)
			i++
			continue
		}
		if inBacktick && ch == '`' && next == '`' {
			sb.WriteByte(ch)
			sb.WriteByte(next)
			i++
			continue
		}

		switch ch {
		case '\'':
			if !inDouble && !inBacktick {
				if !inSingle {
					singleBackslash = hasBackslashEscapePrefix(sqlText, i)
				}
				inSingle = !inSingle
				if !inSingle {
					singleBackslash = false
				}
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		}

		sb.WriteByte(ch)
	}
	appendStatement(&out, sb.String())
	return out
}

func hasBackslashEscapePrefix(text string, quote int) bool {
	if quote <= 0 {
		return false
	}
	if text[quote-1] != 'E' && text[quote-1] != 'e' {
		return false
	}
	if quote == 1 {
		return true
	}
	prev := text[quote-2]
	return !sqlscan.IsIdentPart(prev)
}

func appendStatement(out *[]string, statement string) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		return
	}
	*out = append(*out, statement)
}
