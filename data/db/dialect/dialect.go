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
func (d Dialect) Rebind(query string) string {
	if query == "" {
		return query
	}
	switch d.name {
	case NamePostgres:
		// 简单扫描，将每个 ? 替换为 $n。
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
// 通过错误消息的关键字做最小匹配，覆盖常见数据库：
//   - MySQL: "Duplicate entry", "duplicate key"
//   - SQLite: "UNIQUE constraint failed"
//   - Postgres: "duplicate key value", "unique constraint"
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
