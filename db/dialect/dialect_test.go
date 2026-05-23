package dialect

import (
	"strconv"
	"strings"
	"testing"
)

// TestRebind_Postgres 验证 Rebind Postgres。
func TestRebind_Postgres(t *testing.T) {
	d := New("postgres")
	q := "SELECT * FROM t WHERE a = ? AND b IN (?, ?)"
	got := d.Rebind(q)
	want := "SELECT * FROM t WHERE a = $1 AND b IN ($2, $3)"
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresSkipsLiteralsCommentsAndJSONQuestionOperators(t *testing.T) {
	d := New("postgres")
	q := `SELECT payload ? 'name',
       payload ?| array['a', 'b'],
       payload ?& array['a', 'b'],
       note = '?',
       $$ ? $$,
       -- ? in comment
       id = ?
FROM events
WHERE tenant_id = ? AND payload ? 'enabled' AND (? IS NULL OR status = ?)`
	got := d.Rebind(q)
	want := `SELECT payload ? 'name',
       payload ?| array['a', 'b'],
       payload ?& array['a', 'b'],
       note = '?',
       $$ ? $$,
       -- ? in comment
       id = $1
FROM events
WHERE tenant_id = $2 AND payload ? 'enabled' AND ($3 IS NULL OR status = $4)`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresJSONQuestionOperatorWithPlaceholderValue(t *testing.T) {
	d := New("postgres")
	q := `SELECT * FROM events WHERE payload ? ? AND id = ?`
	got := d.Rebind(q)
	want := `SELECT * FROM events WHERE payload ? $1 AND id = $2`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresQuestionPlaceholdersNearKeywords(t *testing.T) {
	d := New("postgres")
	q := `SELECT * FROM users WHERE name LIKE ? AND deleted_at IS ? AND id = ?`
	got := d.Rebind(q)
	want := `SELECT * FROM users WHERE name LIKE $1 AND deleted_at IS $2 AND id = $3`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresSkipsEscapeStringLiterals(t *testing.T) {
	d := New("postgres")
	q := `SELECT E'it\'s ?' AS note, U&'d\0061t?' AS escaped, id = ?`
	got := d.Rebind(q)
	want := `SELECT E'it\'s ?' AS note, U&'d\0061t?' AS escaped, id = $1`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresHandlesQualifiedNamesAroundPrefixedStrings(t *testing.T) {
	d := New("postgres")
	q := `SELECT schema.payload ? 'name', table.E'it\'s ?' AS note, id = ?`
	got := d.Rebind(q)
	want := `SELECT schema.payload ? 'name', table.E'it\'s ?' AS note, id = $1`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresSkipsBitAndHexStringLiterals(t *testing.T) {
	d := New("postgres")
	q := `SELECT B'10?1' AS bits, X'AB?C' AS hex, id = ?`
	got := d.Rebind(q)
	want := `SELECT B'10?1' AS bits, X'AB?C' AS hex, id = $1`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresSkipsCommentsAroundJSONQuestionOperator(t *testing.T) {
	d := New("postgres")
	q := `SELECT *
FROM events
WHERE payload /* json exists */ ? 'name'
  AND payload -- still JSON operator
  ? 'enabled'
  AND id = ?`
	got := d.Rebind(q)
	want := `SELECT *
FROM events
WHERE payload /* json exists */ ? 'name'
  AND payload -- still JSON operator
  ? 'enabled'
  AND id = $1`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresSkipsNestedBlockComments(t *testing.T) {
	d := New("postgres")
	q := `SELECT 1 /* outer /* inner ? */ still comment ? */, id = ?`
	got := d.Rebind(q)
	want := `SELECT 1 /* outer /* inner ? */ still comment ? */, id = $1`
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestRebind_PostgresLargeParameterizedInsert(t *testing.T) {
	d := New("postgres")
	var query strings.Builder
	var want strings.Builder
	query.WriteString("INSERT INTO events (id, payload) VALUES ")
	want.WriteString("INSERT INTO events (id, payload) VALUES ")
	argIndex := 1
	for i := 0; i < 512; i++ {
		if i > 0 {
			query.WriteString(", ")
			want.WriteString(", ")
		}
		query.WriteString("(?, ?)")
		want.WriteString("($")
		want.WriteString(strconv.Itoa(argIndex))
		want.WriteString(", $")
		want.WriteString(strconv.Itoa(argIndex + 1))
		want.WriteByte(')')
		argIndex += 2
	}
	if got := d.Rebind(query.String()); got != want.String() {
		t.Fatalf("Rebind mismatch on large insert")
	}
}

// TestRebind_NoChangeForMySQLSQLite 验证 Rebind NoChangeForMySQLSQLite。
func TestRebind_NoChangeForMySQLSQLite(t *testing.T) {
	tests := []struct {
		name string
		d    Dialect
	}{
		{"mysql", New("mysql")},
		{"sqlite", New("sqlite")},
		{"unknown", New("unknown")},
	}

	orig := "DELETE FROM t WHERE id = ? AND name = ?"
	for _, tt := range tests {
		if got := tt.d.Rebind(orig); got != orig {
			t.Fatalf("%s: expected no change, got %s", tt.name, got)
		}
	}
}

func TestDialectSupportsSavepoints(t *testing.T) {
	tests := []struct {
		name string
		d    Dialect
		want bool
	}{
		{name: "mysql", d: New("mysql"), want: true},
		{name: "sqlite", d: New("sqlite"), want: true},
		{name: "postgres", d: New("postgres"), want: true},
		{name: "unknown", d: New("sqlserver"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.SupportsSavepoints(); got != tt.want {
				t.Fatalf("SupportsSavepoints() = %v, want %v", got, tt.want)
			}
		})
	}
}
