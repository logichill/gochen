package sql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	core "gochen/storage/database"
	"gochen/storage/database/dialect"
)

type upsertBuilder struct {
	db      core.IDatabase
	dialect dialect.Dialect

	table        string
	columns      []string
	values       []any
	keyColumns   []string
	updateValues map[string]any
}

func (b *upsertBuilder) Columns(cols ...string) IUpsertBuilder {
	b.columns = cols
	return b
}

func (b *upsertBuilder) Values(vals ...any) IUpsertBuilder {
	b.values = vals
	return b
}

func (b *upsertBuilder) Key(cols ...string) IUpsertBuilder {
	b.keyColumns = cols
	return b
}

func (b *upsertBuilder) UpdateSet(col string, val any) IUpsertBuilder {
	if b.updateValues == nil {
		b.updateValues = make(map[string]any)
	}
	b.updateValues[col] = val
	return b
}

func (b *upsertBuilder) UpdateSetMap(values map[string]any) IUpsertBuilder {
	if b.updateValues == nil {
		b.updateValues = make(map[string]any)
	}
	for k, v := range values {
		b.updateValues[k] = v
	}
	return b
}

func (b *upsertBuilder) Exec(ctx context.Context) (sql.Result, error) {
	if len(b.columns) == 0 {
		return nil, fmt.Errorf("upsert: Columns is required")
	}
	if len(b.values) != len(b.columns) {
		return nil, fmt.Errorf("upsert: values length mismatch columns length")
	}
	if len(b.keyColumns) == 0 {
		return nil, fmt.Errorf("upsert: Key is required")
	}

	ins := &insertBuilder{
		db:      b.db,
		dialect: b.dialect,
		table:   b.table,
		columns: b.columns,
		rows:    [][]any{b.values},
	}

	insertSQL, insertArgs := ins.Build()
	res, err := b.db.Exec(ctx, insertSQL, insertArgs...)
	if err == nil {
		return res, nil
	}

	if !b.dialect.IsUniqueViolation(err) {
		return nil, err
	}

	whereParts := make([]string, 0, len(b.keyColumns))
	whereArgs := make([]any, 0, len(b.keyColumns))

	colIndex := func(col string) int {
		for i, c := range b.columns {
			if c == col {
				return i
			}
		}
		return -1
	}

	for _, key := range b.keyColumns {
		idx := colIndex(key)
		if idx < 0 {
			return nil, fmt.Errorf("upsert: key column %s not found in Columns", key)
		}
		whereParts = append(whereParts, key+" = ?")
		whereArgs = append(whereArgs, b.values[idx])
	}

	updateVals := make(map[string]any)
	if len(b.updateValues) == 0 {
		for i, col := range b.columns {
			skip := false
			for _, key := range b.keyColumns {
				if key == col {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			updateVals[col] = b.values[i]
		}
	} else {
		updateVals = b.updateValues
	}

	upd := &updateBuilder{
		db:      b.db,
		dialect: b.dialect,
		table:   b.table,
	}
	upd.SetMap(updateVals)
	upd.Where(strings.Join(whereParts, " AND "), whereArgs...)

	updateSQL, updateArgs := upd.Build()
	return b.db.Exec(ctx, updateSQL, updateArgs...)
}
