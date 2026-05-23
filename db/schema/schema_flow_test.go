package schema_test

import (
	"context"
	"path/filepath"
	"testing"

	core "gochen/db"
	"gochen/db/dialect"
	"gochen/db/schema"
	schemadiff "gochen/db/schema/diff"
	"gochen/db/schema/introspect"
	"gochen/db/schema/render"
	"gochen/db/sql/stdsql"

	_ "modernc.org/sqlite"
)

func TestDiffRenderAndInspectSQLiteFlow(t *testing.T) {
	t.Parallel()

	database := openSchemaFlowDB(t)
	execSchemaFlowSQL(t, database, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT)`)

	current, err := introspect.Inspect(context.Background(), database)
	if err != nil {
		t.Fatalf("Inspect current failed: %v", err)
	}
	desired := &schema.Schema{Tables: []schema.Table{
		{
			Name: "users",
			Columns: []schema.Column{
				{Name: "id", Type: "INTEGER", PrimaryKey: true, AutoIncrement: true},
				{Name: "email", Type: "TEXT", Nullable: false, DefaultValue: "''"},
			},
			Indexes: []schema.Index{{Name: "idx_users_email", Columns: []string{"email"}, Unique: true}},
		},
	}}

	changes := schemadiff.Between(current, desired)
	sqls, err := render.RenderSQL(changes, dialect.New("sqlite"))
	if err != nil {
		t.Fatalf("RenderSQL failed: %v", err)
	}
	for _, stmt := range sqls {
		execSchemaFlowSQL(t, database, stmt)
	}

	after, err := introspect.Inspect(context.Background(), database)
	if err != nil {
		t.Fatalf("Inspect after failed: %v", err)
	}
	users, ok := after.FindTable("", "users")
	if !ok {
		t.Fatal("expected users table")
	}
	if _, ok := users.FindColumn("email"); !ok {
		t.Fatal("expected email column")
	}
	if idx, ok := users.FindIndex("idx_users_email"); !ok || !idx.Unique {
		t.Fatalf("expected unique email index, got %#v ok=%v", idx, ok)
	}
}

func openSchemaFlowDB(t *testing.T) core.IDatabase {
	t.Helper()
	database, err := stdsql.NewWithContext(context.Background(), core.DBConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "schema-flow.db"),
	})
	if err != nil {
		t.Fatalf("open sqlite db failed: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func execSchemaFlowSQL(t *testing.T, database core.IDatabase, stmt string) {
	t.Helper()
	if _, err := database.Exec(context.Background(), stmt); err != nil {
		t.Fatalf("Exec %q failed: %v", stmt, err)
	}
}
