package dialect

import "testing"

func TestRebind_Postgres(t *testing.T) {
	d := New("postgres")
	q := "SELECT * FROM t WHERE a = ? AND b IN (?, ?)"
	got := d.Rebind(q)
	want := "SELECT * FROM t WHERE a = $1 AND b IN ($2, $3)"
	if got != want {
		t.Fatalf("Rebind mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

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
