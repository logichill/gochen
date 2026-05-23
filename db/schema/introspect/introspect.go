package introspect

import (
	"context"
	"fmt"

	"gochen/db"
	"gochen/db/dialect"
	"gochen/db/schema"
	goerrors "gochen/errors"
)

// IIntrospector 抽象数据库结构读取器。
type IIntrospector interface {
	// Inspect 读取数据库当前 schema 快照。
	Inspect(ctx context.Context, database db.IDatabase) (*schema.Schema, error)
}

// Inspect 根据数据库方言选择默认 introspector。
func Inspect(ctx context.Context, database db.IDatabase) (*schema.Schema, error) {
	if database == nil {
		return nil, goerrors.NewCode(goerrors.InvalidInput, "database cannot be nil")
	}
	switch dialect.FromDatabase(database).Name() {
	case dialect.NameSQLite:
		return NewSQLite().Inspect(ctx, database)
	case dialect.NameMySQL:
		return NewMySQL().Inspect(ctx, database)
	case dialect.NamePostgres:
		return NewPostgres().Inspect(ctx, database)
	default:
		return nil, fmt.Errorf("%w: %s", goerrors.ErrUnsupported, dialect.FromDatabase(database).Name())
	}
}
