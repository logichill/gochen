package basic

import "database/sql"

// Rows 包装 sql.Rows 以实现 core.IRows
type Rows struct{ rows *sql.Rows }

func (r *Rows) Next() bool                              { return r.rows.Next() }
func (r *Rows) Scan(dest ...interface{}) error          { return r.rows.Scan(dest...) }
func (r *Rows) Close() error                            { return r.rows.Close() }
func (r *Rows) Err() error                              { return r.rows.Err() }
func (r *Rows) Columns() ([]string, error)              { return r.rows.Columns() }
func (r *Rows) ColumnTypes() ([]*sql.ColumnType, error) { return r.rows.ColumnTypes() }

// Row 包装 sql.Row 以实现 core.IRow
type Row struct{ row *sql.Row }

func (r *Row) Scan(dest ...interface{}) error { return r.row.Scan(dest...) }
func (r *Row) Err() error                     { return nil }
