package sqlstore

import (
	"fmt"
	"strings"

	"gochen/db"
	"gochen/db/dialect"
	"gochen/db/sql/safeident"
	"gochen/errors"
)

func normalizeSQLTableName(database db.IDatabase, tableName, fallback string) (string, error) {
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = fallback
	}
	if !safeident.IsSafeIdentifier(tableName) {
		return "", errors.NewCode(errors.InvalidInput, fmt.Sprintf("invalid SQL table name: %s", tableName))
	}
	return dialect.FromDatabase(database).QuoteIdentifier(tableName), nil
}
