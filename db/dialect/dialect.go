package dialect

import (
	"strconv"
	"strings"

	core "gochen/db"
	"gochen/db/internal/sqlscan"
)

// Name 标准化的数据库方言名称。
type Name string

const (
	// NameMySQL 表示 MySQL 方言。
	NameMySQL Name = "mysql"
	// NameSQLite 表示 SQLite 方言。
	NameSQLite Name = "sqlite"
	// NamePostgres 表示 PostgreSQL 方言。
	NamePostgres Name = "postgres"
	// NameUnknown 表示无法识别的方言。
	NameUnknown Name = ""
)

// Dialect 表示当前数据库的方言能力。
//
// 目前只抽象项目实际用到的能力：
//   - DeleteLimit: 是否支持 DELETE ... LIMIT
//   - UniqueViolation: 唯一键/主键冲突错误识别。
type Dialect struct {
	name Name
}

// New 根据驱动名构造一个标准化的方言描述对象。
func New(name string) Dialect {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mysql":
		return Dialect{name: NameMySQL}
	case "sqlite", "sqlite3":
		return Dialect{name: NameSQLite}
	case "postgres", "postgresql":
		return Dialect{name: NamePostgres}
	default:
		return Dialect{name: NameUnknown}
	}
}

// FromDatabase 尝试从数据库适配器推断方言；无法识别时回退为 Unknown。
func FromDatabase(db core.IDatabase) Dialect {
	if db == nil {
		return Dialect{name: NameUnknown}
	}
	if p, ok := db.(core.IDialectNameProvider); ok {
		return New(p.DialectName())
	}
	return Dialect{name: NameUnknown}
}

func (d Dialect) Name() Name {
	return d.name
}

// QuoteIdentifier 按当前方言为表名或列名加上合适的引号。
func (d Dialect) QuoteIdentifier(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.Split(name, ".")
	for i, p := range parts {
		if p == "" {
			continue
		}
		switch d.name {
		case NameMySQL:
			parts[i] = "`" + p + "`"
		case NameSQLite, NamePostgres:
			parts[i] = `"` + p + `"`
		default:
			// 未知方言：保持原样
		}
	}
	return strings.Join(parts, ".")
}

// Rebind 把通用 `?` 占位符转换成当前方言所需的形式。
func (d Dialect) Rebind(query string) string {
	if query == "" {
		return query
	}
	switch d.name {
	case NamePostgres:
		var sb strings.Builder
		sb.Grow(len(query) + 4)
		argIndex := 1
		prevScanner := newSQLPrevTokenScanner(query)
		for i := 0; i < len(query); {
			ch := query[i]
			if isPrefixedStringStart(ch) {
				if next, ok := scanPostgresPrefixedString(query, i); ok {
					sb.WriteString(query[i:next])
					i = next
					continue
				}
			}
			if ch == '\'' {
				next := sqlscan.ScanSingleQuoted(query, i)
				sb.WriteString(query[i:next])
				i = next
				continue
			}
			if ch == '"' {
				next := sqlscan.ScanQuoted(query, i, '"')
				sb.WriteString(query[i:next])
				i = next
				continue
			}
			if ch == '$' {
				if next, ok := sqlscan.ScanDollarQuoted(query, i); ok {
					sb.WriteString(query[i:next])
					i = next
					continue
				}
			}
			if ch == '-' && i+1 < len(query) && query[i+1] == '-' {
				next := sqlscan.ScanLineComment(query, i)
				sb.WriteString(query[i:next])
				i = next
				continue
			}
			if ch == '/' && i+1 < len(query) && query[i+1] == '*' {
				next := sqlscan.ScanNestedBlockComment(query, i)
				sb.WriteString(query[i:next])
				i = next
				continue
			}
			if ch == '?' && !isPostgresJSONQuestionOperator(query, &prevScanner, i) {
				sb.WriteByte('$')
				sb.WriteString(strconv.Itoa(argIndex))
				argIndex++
			} else {
				sb.WriteByte(ch)
			}
			i++
		}
		return sb.String()
	default:
		return query
	}
}

func scanPostgresPrefixedString(query string, start int) (int, bool) {
	if start > 0 && sqlscan.IsIdentPart(query[start-1]) {
		return start, false
	}
	if start+1 < len(query) && (query[start] == 'E' || query[start] == 'e') && query[start+1] == '\'' {
		return sqlscan.ScanBackslashSingleQuoted(query, start+1), true
	}
	if start+2 < len(query) && (query[start] == 'U' || query[start] == 'u') && query[start+1] == '&' && query[start+2] == '\'' {
		return sqlscan.ScanSingleQuoted(query, start+2), true
	}
	if start+1 < len(query) && (query[start] == 'B' || query[start] == 'b' || query[start] == 'X' || query[start] == 'x') && query[start+1] == '\'' {
		return sqlscan.ScanSingleQuoted(query, start+1), true
	}
	return start, false
}

type sqlTokenBounds struct {
	start int
	end   int
}

type sqlPrevTokenScanner struct {
	query string
	next  int
	last  sqlTokenBounds
}

func newSQLPrevTokenScanner(query string) sqlPrevTokenScanner {
	return sqlPrevTokenScanner{
		query: query,
		last:  sqlTokenBounds{start: -1, end: -1},
	}
}

func (s *sqlPrevTokenScanner) previousBefore(before int) (int, int, bool) {
	if s == nil || before <= 0 {
		return 0, 0, false
	}
	for s.next < before {
		start, end, next, ok := scanSQLToken(s.query, s.next)
		if !ok || start >= before {
			break
		}
		s.last = sqlTokenBounds{start: start, end: end}
		s.next = next
	}
	if s.last.start < 0 {
		return 0, 0, false
	}
	return s.last.start, s.last.end, true
}

func isPostgresJSONQuestionOperator(query string, prevScanner *sqlPrevTokenScanner, index int) bool {
	if index+1 < len(query) && (query[index+1] == '|' || query[index+1] == '&') {
		return true
	}
	prevStart, prevEnd, ok := prevScanner.previousBefore(index)
	if !ok {
		return false
	}
	if isPlaceholderContextToken(query[prevStart:prevEnd]) {
		return false
	}
	if !isPostgresJSONLeftOperand(query[prevStart:prevEnd]) {
		return false
	}
	nextStart, nextEnd, ok := nextSQLToken(query, index+1)
	if !ok {
		return false
	}
	if isPlaceholderContextToken(query[nextStart:nextEnd]) {
		return false
	}
	return isSQLValueStart(query[nextStart])
}

func nextSQLToken(query string, start int) (int, int, bool) {
	tokenStart, tokenEnd, _, ok := scanSQLToken(query, start)
	if !ok {
		return 0, 0, false
	}
	return tokenStart, tokenEnd, true
}

func scanSQLToken(query string, start int) (int, int, int, bool) {
	i := skipSQLTrivia(query, start)
	if i >= len(query) {
		return 0, 0, 0, false
	}
	if next, ok := scanPostgresPrefixedString(query, i); ok {
		return i, next, next, true
	}
	switch query[i] {
	case '\'':
		next := sqlscan.ScanSingleQuoted(query, i)
		return i, next, next, true
	case '"':
		next := sqlscan.ScanQuoted(query, i, '"')
		return i, next, next, true
	case '$':
		if next, ok := sqlscan.ScanDollarQuoted(query, i); ok {
			return i, next, next, true
		}
	}
	if sqlscan.IsIdentPart(query[i]) {
		start = i
		for i < len(query) && sqlscan.IsIdentPart(query[i]) {
			i++
		}
		return start, i, i, true
	}
	return i, i + 1, i + 1, true
}

func skipSQLTrivia(query string, start int) int {
	for i := start; i < len(query); {
		if sqlscan.IsSpace(query[i]) {
			i++
			continue
		}
		if query[i] == '-' && i+1 < len(query) && query[i+1] == '-' {
			i = sqlscan.ScanLineComment(query, i)
			continue
		}
		if query[i] == '/' && i+1 < len(query) && query[i+1] == '*' {
			i = sqlscan.ScanNestedBlockComment(query, i)
			continue
		}
		return i
	}
	return len(query)
}

func isPlaceholderContextToken(token string) bool {
	switch strings.ToUpper(strings.TrimSpace(token)) {
	case "WHERE", "AND", "OR", "NOT", "ON", "WHEN", "THEN", "ELSE", "BY", "SELECT", "VALUES", "SET", "RETURNING", "HAVING", "CASE", "IN", "AS", "FROM", "JOIN", "USING", "LIMIT", "OFFSET", "LIKE", "ILIKE", "IS", "TO", "BETWEEN", "ESCAPE":
		return true
	default:
		return false
	}
}

func isPostgresJSONLeftOperand(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	last := token[len(token)-1]
	return sqlscan.IsIdentPart(last) || last == '\'' || last == '"' || last == ')' || last == ']'
}

func isSQLValueStart(ch byte) bool {
	return ch == '?' || ch == '\'' || ch == '"' || ch == '$' || ch == '(' || sqlscan.IsIdentStart(ch) || (ch >= '0' && ch <= '9')
}

func isPrefixedStringStart(ch byte) bool {
	switch ch {
	case 'E', 'e', 'U', 'u', 'B', 'b', 'X', 'x':
		return true
	default:
		return false
	}
}

// SupportsDeleteLimit 返回当前方言是否支持 `DELETE ... LIMIT`。
func (d Dialect) SupportsDeleteLimit() bool {
	switch d.name {
	case NameMySQL, NameSQLite:
		return true
	default:
		return false
	}
}

// SupportsSavepoints 当前方言是否支持 savepoint 语义。
func (d Dialect) SupportsSavepoints() bool {
	switch d.name {
	case NameMySQL, NameSQLite, NamePostgres:
		return true
	default:
		return false
	}
}

// IsUniqueViolation 用常见错误文本特征判断是否发生了唯一键冲突。
func (d Dialect) IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch d.name {
	case NameMySQL:
		return strings.Contains(msg, "duplicate entry") ||
			strings.Contains(msg, "duplicate key")
	case NameSQLite:
		return strings.Contains(msg, "unique constraint failed")
	case NamePostgres:
		return strings.Contains(msg, "duplicate key") ||
			strings.Contains(msg, "unique constraint")
	default:
		// 对未知方言做宽松匹配，尽量不误判但宁可返回 false
		return strings.Contains(msg, "duplicate key") ||
			strings.Contains(msg, "unique constraint")
	}
}
