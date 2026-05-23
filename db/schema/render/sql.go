package render

import (
	"fmt"
	"strings"

	"gochen/db/dialect"
	"gochen/db/schema"
	schemadiff "gochen/db/schema/diff"
	"gochen/db/sql/safeident"
)

// RenderSQL 把 schema 变更渲染为指定方言的 SQL。
func RenderSQL(changes schemadiff.Changes, d dialect.Dialect) ([]string, error) {
	var statements []string
	for _, change := range changes {
		switch change.Kind {
		case schemadiff.KindAddTable:
			tableName, err := addTableName(change)
			if err != nil {
				return nil, err
			}
			statement, err := renderCreateTable(change, d)
			if err != nil {
				return nil, err
			}
			statements = append(statements, statement)
			if change.Target != nil {
				for _, index := range change.Target.Indexes {
					idx := index
					stmt, err := renderCreateIndex(tableName, &idx, d)
					if err != nil {
						return nil, err
					}
					statements = append(statements, stmt)
				}
			}
		case schemadiff.KindAddColumn:
			statement, err := renderAddColumn(change, d)
			if err != nil {
				return nil, err
			}
			statements = append(statements, statement)
		case schemadiff.KindAddIndex:
			statement, err := renderAddIndex(change, d)
			if err != nil {
				return nil, err
			}
			statements = append(statements, statement)
		default:
			return nil, fmt.Errorf("unsupported schema change kind: %s", change.Kind)
		}
	}
	return statements, nil
}

func addTableName(change schemadiff.Change) (string, error) {
	if change.Target == nil {
		return "", fmt.Errorf("add table change missing target table")
	}
	tableName := strings.TrimSpace(change.Target.QualifiedName())
	if strings.TrimSpace(change.Table) != "" && strings.TrimSpace(change.Table) != tableName {
		return "", fmt.Errorf("add table change table mismatch: table=%q target=%q", change.Table, tableName)
	}
	return tableName, nil
}

// RenderDownSQL 把新增类 schema 变更反向渲染为回滚 SQL。
func RenderDownSQL(changes schemadiff.Changes, d dialect.Dialect) ([]string, error) {
	var statements []string
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		switch change.Kind {
		case schemadiff.KindAddTable:
			statement, err := renderDropTable(change, d)
			if err != nil {
				return nil, err
			}
			statements = append(statements, statement)
		case schemadiff.KindAddColumn:
			statement, err := renderDropColumn(change, d)
			if err != nil {
				return nil, err
			}
			statements = append(statements, statement)
		case schemadiff.KindAddIndex:
			statement, err := renderDropIndex(change, d)
			if err != nil {
				return nil, err
			}
			statements = append(statements, statement)
		default:
			return nil, fmt.Errorf("unsupported schema change kind: %s", change.Kind)
		}
	}
	return statements, nil
}

func renderCreateTable(change schemadiff.Change, d dialect.Dialect) (string, error) {
	if change.Target == nil {
		return "", fmt.Errorf("add table change missing target table")
	}
	tableName := change.Target.QualifiedName()
	if err := validateQualifiedIdentifier(tableName, "table"); err != nil {
		return "", err
	}
	if len(change.Target.Columns) == 0 {
		return "", fmt.Errorf("table %s has no columns", tableName)
	}
	if err := validateCreateTablePrimaryKeyLayout(change.Target.Columns, d); err != nil {
		return "", err
	}

	defs := make([]string, 0, len(change.Target.Columns)+1)
	var primary []string
	inlinePrimaryKeyColumn := inlinePrimaryKeyColumnName(change.Target.Columns, d)
	for _, column := range change.Target.Columns {
		def, inlinePK, err := renderColumn(columnSpec{
			Column:               column,
			TableCreating:        true,
			InlinePrimaryKeyName: inlinePrimaryKeyColumn,
		}, d)
		if err != nil {
			return "", err
		}
		defs = append(defs, def)
		if column.PrimaryKey && !inlinePK {
			if err := validateSimpleIdentifier(column.Name, "primary key column"); err != nil {
				return "", err
			}
			primary = append(primary, d.QuoteIdentifier(column.Name))
		}
	}
	if len(primary) > 0 {
		defs = append(defs, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(primary, ", ")))
	}

	return fmt.Sprintf(
		"CREATE TABLE %s (\n  %s\n)",
		d.QuoteIdentifier(tableName),
		strings.Join(defs, ",\n  "),
	), nil
}

func renderDropTable(change schemadiff.Change, d dialect.Dialect) (string, error) {
	if err := validateQualifiedIdentifier(change.Table, "table"); err != nil {
		return "", err
	}
	return fmt.Sprintf("DROP TABLE %s", d.QuoteIdentifier(change.Table)), nil
}

func renderAddColumn(change schemadiff.Change, d dialect.Dialect) (string, error) {
	if err := validateQualifiedIdentifier(change.Table, "table"); err != nil {
		return "", err
	}
	if change.Column == nil {
		return "", fmt.Errorf("add column change missing column")
	}
	def, _, err := renderColumn(columnSpec{Column: *change.Column}, d)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN %s",
		d.QuoteIdentifier(change.Table),
		def,
	), nil
}

func renderDropColumn(change schemadiff.Change, d dialect.Dialect) (string, error) {
	if err := validateQualifiedIdentifier(change.Table, "table"); err != nil {
		return "", err
	}
	if change.Column == nil {
		return "", fmt.Errorf("drop column change missing column")
	}
	if err := validateSimpleIdentifier(change.Column.Name, "column"); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"ALTER TABLE %s DROP COLUMN %s",
		d.QuoteIdentifier(change.Table),
		d.QuoteIdentifier(change.Column.Name),
	), nil
}

func renderAddIndex(change schemadiff.Change, d dialect.Dialect) (string, error) {
	return renderCreateIndex(change.Table, change.Index, d)
}

func renderCreateIndex(table string, index *schema.Index, d dialect.Dialect) (string, error) {
	if index == nil {
		return "", fmt.Errorf("add index change missing index")
	}
	if err := validateQualifiedIdentifier(table, "table"); err != nil {
		return "", err
	}
	if err := validateSimpleIdentifier(index.Name, "index"); err != nil {
		return "", err
	}
	if len(index.Columns) == 0 {
		return "", fmt.Errorf("index %s has no columns", index.Name)
	}

	quotedColumns := make([]string, 0, len(index.Columns))
	for _, column := range index.Columns {
		column = strings.TrimSpace(column)
		if err := validateSimpleIdentifier(column, "index column"); err != nil {
			return "", err
		}
		quotedColumns = append(quotedColumns, d.QuoteIdentifier(column))
	}

	unique := ""
	if index.Unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf(
		"CREATE %sINDEX %s ON %s (%s)",
		unique,
		d.QuoteIdentifier(index.Name),
		d.QuoteIdentifier(table),
		strings.Join(quotedColumns, ", "),
	), nil
}

func renderDropIndex(change schemadiff.Change, d dialect.Dialect) (string, error) {
	if err := validateQualifiedIdentifier(change.Table, "table"); err != nil {
		return "", err
	}
	if change.Index == nil {
		return "", fmt.Errorf("drop index change missing index")
	}
	if err := validateSimpleIdentifier(change.Index.Name, "index"); err != nil {
		return "", err
	}
	switch d.Name() {
	case dialect.NameMySQL:
		return fmt.Sprintf(
			"DROP INDEX %s ON %s",
			d.QuoteIdentifier(change.Index.Name),
			d.QuoteIdentifier(change.Table),
		), nil
	case dialect.NamePostgres:
		schemaName, _ := schema.ParseTableName(change.Table)
		if schemaName != "" {
			return fmt.Sprintf(
				"DROP INDEX %s.%s",
				d.QuoteIdentifier(schemaName),
				d.QuoteIdentifier(change.Index.Name),
			), nil
		}
		return fmt.Sprintf("DROP INDEX %s", d.QuoteIdentifier(change.Index.Name)), nil
	default:
		return fmt.Sprintf("DROP INDEX %s", d.QuoteIdentifier(change.Index.Name)), nil
	}
}

type columnSpec struct {
	Column               schema.Column
	TableCreating        bool
	InlinePrimaryKeyName string
}

func renderColumn(spec columnSpec, d dialect.Dialect) (definition string, inlinePrimaryKey bool, err error) {
	column := spec.Column
	if err := validateSimpleIdentifier(column.Name, "column"); err != nil {
		return "", false, err
	}
	typ := strings.TrimSpace(column.Type)
	if typ == "" {
		return "", false, fmt.Errorf("column %s type cannot be empty", column.Name)
	}

	parts := []string{d.QuoteIdentifier(column.Name), renderColumnType(column, d)}
	inlinePK := false
	if column.PrimaryKey && column.AutoIncrement && spec.TableCreating && spec.InlinePrimaryKeyName == column.Name {
		inlinePK = true
		switch d.Name() {
		case dialect.NameSQLite:
			parts = []string{d.QuoteIdentifier(column.Name), "INTEGER PRIMARY KEY AUTOINCREMENT"}
		default:
			inlinePK = false
		}
	} else if column.PrimaryKey && spec.TableCreating {
		inlinePK = false
	} else if column.PrimaryKey {
		return "", false, fmt.Errorf("adding primary key column is not supported")
	} else if column.AutoIncrement && d.Name() == dialect.NameSQLite {
		return "", false, fmt.Errorf("sqlite auto increment column %s must be primary key", column.Name)
	} else if column.AutoIncrement && d.Name() == dialect.NameMySQL {
		return "", false, fmt.Errorf("mysql auto increment column %s must be primary key", column.Name)
	}

	if !inlinePK {
		if !column.Nullable || column.AutoIncrement {
			parts = append(parts, "NOT NULL")
		}
		if column.AutoIncrement && d.Name() == dialect.NameMySQL {
			parts = append(parts, "AUTO_INCREMENT")
		}
		if strings.TrimSpace(column.DefaultValue) != "" && !column.AutoIncrement {
			parts = append(parts, "DEFAULT", strings.TrimSpace(column.DefaultValue))
		}
	}

	return strings.Join(parts, " "), inlinePK, nil
}

func validateCreateTablePrimaryKeyLayout(columns []schema.Column, d dialect.Dialect) error {
	if d.Name() != dialect.NameSQLite {
		return nil
	}
	primaryCount := 0
	autoIncrementPrimaryCount := 0
	for _, column := range columns {
		if !column.PrimaryKey {
			continue
		}
		primaryCount++
		if column.AutoIncrement {
			autoIncrementPrimaryCount++
		}
	}
	if primaryCount > 1 && autoIncrementPrimaryCount > 0 {
		return fmt.Errorf("sqlite auto increment column requires a single-column primary key")
	}
	if autoIncrementPrimaryCount > 1 {
		return fmt.Errorf("sqlite supports at most one auto increment primary key column")
	}
	return nil
}

func inlinePrimaryKeyColumnName(columns []schema.Column, d dialect.Dialect) string {
	if d.Name() != dialect.NameSQLite {
		return ""
	}
	for _, column := range columns {
		if column.PrimaryKey && column.AutoIncrement {
			return column.Name
		}
	}
	return ""
}

func validateQualifiedIdentifier(name string, source string) error {
	schemaName, simpleName := schema.ParseTableName(name)
	if simpleName == "" || !safeident.IsSafeIdentifier(simpleName) || strings.Contains(simpleName, ".") {
		return fmt.Errorf("unsafe schema %s identifier: %q", source, name)
	}
	if schemaName != "" && (!safeident.IsSafeIdentifier(schemaName) || strings.Contains(schemaName, ".")) {
		return fmt.Errorf("unsafe schema %s identifier: %q", source, name)
	}
	return nil
}

func validateSimpleIdentifier(name string, source string) error {
	name = strings.TrimSpace(name)
	if strings.Contains(name, ".") || !safeident.IsSafeIdentifier(name) {
		return fmt.Errorf("unsafe schema %s identifier: %q", source, name)
	}
	return nil
}

func renderColumnType(column schema.Column, d dialect.Dialect) string {
	typ := strings.TrimSpace(column.Type)
	if !column.AutoIncrement {
		return typ
	}
	if d.Name() == dialect.NamePostgres {
		switch strings.ToUpper(typ) {
		case "SMALLINT", "INT2":
			return "SMALLSERIAL"
		case "BIGINT", "INT8":
			return "BIGSERIAL"
		case "INTEGER", "INTEGER4", "INT", "INT4":
			return "SERIAL"
		}
	}
	return typ
}
