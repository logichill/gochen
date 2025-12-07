package dialect

import (
	"strconv"
	"strings"

	core "gochen/data/db"
)

// Name 标准化的数据库方言名称
type Name string

const (
	NameMySQL    Name = "mysql"
	NameSQLite   Name = "sqlite"
	NamePostgres Name = "postgres"
	NameUnknown  Name = ""
)

// Dialect 表示当前数据库的方言能力
//
// 目前只抽象项目实际用到的能力：
//   - DeleteLimit: 是否支持 DELETE ... LIMIT
//   - UniqueViolation: 唯一键/主键冲突错误识别
type Dialect struct {
	name Name
}

// New 根据字符串构造方言（大小写不敏感）
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

// FromDatabase 从 IDatabase 实例推断方言
//
// 需要 IDatabase 可选实现 IDialectNameProvider 接口；否则返回 Unknown。
func FromDatabase(db core.IDatabase) Dialect {
	if db == nil {
		return Dialect{name: NameUnknown}
	}
	if p, ok := db.(core.IDialectNameProvider); ok {
		return New(p.GetDialectName())
	}
	return Dialect{name: NameUnknown}
}

// Name 返回标准化方言名
func (d Dialect) Name() Name {
	return d.name
}

// QuoteIdentifier 根据方言对标识符进行转义（如表名/列名）。
//
// 约定：
//   - 支持 schema.table、table.column 等带点形式，会对每一段分别加引号；
//   - MySQL 使用反引号 `name`，Postgres/SQLite 使用双引号 "name"；
//   - Unknown 方言返回原始字符串，不做修改。
//   - 该方法不负责校验标识符语法，仅负责按方言加引号。
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

// Rebind 将通用占位符 ? 转换为方言特定形式。
//
// 目前仅对 Postgres 做替换，将 ? 依次替换为 $1、$2...；
// 其他方言保持原样。
//
// 重要限制：
// - 当前实现使用简单的字符扫描，不解析 SQL 语法
// - 不区分占位符 ? 是在代码中还是在字符串字面量中
// - 如果 SQL 中包含字符串字面量（如 WHERE name = 'what?'），其中的 ? 也会被替换
//
// 使用建议：
// - 避免在 SQL 字符串字面量中使用 ? 字符
// - 如果必须使用，建议使用参数化方式：WHERE name = ? 并传入 'what?'
// - 或在调用 Rebind 前手动处理字符串字面量
//
// 示例：
//   正确：dialect.Rebind("SELECT * FROM users WHERE name = ?")
//   错误：dialect.Rebind("SELECT * FROM users WHERE name = 'what?'") // ? 会被误替换
func (d Dialect) Rebind(query string) string {
	if query == "" {
		return query
	}
	switch d.name {
	case NamePostgres:
		// 简单扫描，将每个 ? 替换为 $n。
		// 注意：这不处理字符串字面量中的 ?，请参考上述使用建议。
		var sb strings.Builder
		sb.Grow(len(query) + 4) // 粗略预留
		argIndex := 1
		for i := 0; i < len(query); i++ {
			ch := query[i]
			if ch == '?' {
				sb.WriteByte('$')
				sb.WriteString(strconv.Itoa(argIndex))
				argIndex++
			} else {
				sb.WriteByte(ch)
			}
		}
		return sb.String()
	default:
		return query
	}
}

// SupportsDeleteLimit 当前方言是否支持 DELETE ... LIMIT 语法
func (d Dialect) SupportsDeleteLimit() bool {
	switch d.name {
	case NameMySQL, NameSQLite:
		return true
	default:
		return false
	}
}

// IsUniqueViolation 判断错误是否为唯一键/主键冲突
//
// 实现说明：
// - 使用错误消息的关键字匹配，覆盖常见数据库的典型错误格式
// - 这种方式对于轻量级方言抽象来说是可接受的权衡
// - 对于生产环境，如需更精确的判断，建议：
//   1. 使用数据库驱动提供的特定错误类型（如 mysql.MySQLError.Number == 1062）
//   2. 或在业务层实现自定义的错误分类逻辑
//
// 支持的数据库及其错误特征：
//   - MySQL: "Duplicate entry", "duplicate key" (Error 1062, 1586)
//   - SQLite: "UNIQUE constraint failed" (SQLITE_CONSTRAINT_UNIQUE)
//   - Postgres: "duplicate key value", "unique constraint" (Error 23505)
//
// 注意事项：
// - 依赖错误消息文本可能受数据库版本、语言设置影响
// - 如果错误消息格式变化，可能导致误判
// - 建议在单元测试中验证各数据库的实际错误格式
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
