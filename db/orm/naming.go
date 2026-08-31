package orm

import "gochen/db"

// NamingConvention 描述数据表全部切面列名的命名规范约定。
type NamingConvention = db.NamingConvention

// INamingConventionProvider 允许 ORM 或 Database 实例暴露全局命名规范。
type INamingConventionProvider = db.INamingConventionProvider

// DefaultNamingConvention 返回框架内置默认列名规范。
var DefaultNamingConvention = db.DefaultNamingConvention
