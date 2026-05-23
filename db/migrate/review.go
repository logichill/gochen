package migrate

import "strings"

// ManualReviewGuardFunction 是保护生成类破坏性 down SQL 的哨兵函数名。
const ManualReviewGuardFunction = "gochen_migration_down_requires_manual_review"

// ManualReviewGuardStatement 是需要人工 review 后手动删除的保护 SQL 语句。
const ManualReviewGuardStatement = "SELECT " + ManualReviewGuardFunction + "()"

func containsManualReviewGuard(sqlText string) bool {
	target := compactGuardStatement(ManualReviewGuardStatement)
	for _, statement := range SplitStatements(sqlText) {
		if compactGuardStatement(statement) == target {
			return true
		}
	}
	return false
}

func compactGuardStatement(statement string) string {
	var (
		sb                strings.Builder
		inSingle          bool
		inDouble          bool
		inBacktick        bool
		inLineComment     bool
		blockCommentDepth int
	)
	for i := 0; i < len(statement); i++ {
		ch := statement[i]
		next := byte(0)
		if i+1 < len(statement) {
			next = statement[i+1]
		}
		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if blockCommentDepth > 0 {
			if ch == '/' && next == '*' {
				i++
				blockCommentDepth++
				continue
			}
			if ch == '*' && next == '/' {
				i++
				blockCommentDepth--
			}
			continue
		}
		if !inSingle && !inDouble && !inBacktick {
			if ch == '-' && next == '-' {
				i++
				inLineComment = true
				continue
			}
			if ch == '#' {
				inLineComment = true
				continue
			}
			if ch == '/' && next == '*' {
				i++
				blockCommentDepth = 1
				continue
			}
		}
		if inSingle && ch == '\\' && next != 0 {
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
		switch ch {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
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
		if isGuardSpace(ch) || ch == ';' {
			continue
		}
		sb.WriteByte(toLowerASCII(ch))
	}
	return sb.String()
}

func isGuardSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func toLowerASCII(ch byte) byte {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}
