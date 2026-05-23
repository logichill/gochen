package outbox

import (
	"database/sql/driver"
	"testing"

	"gochen/errors"
)

type unsupportedRowsAffectedResult struct{}

func (unsupportedRowsAffectedResult) LastInsertId() (int64, error) { return 0, nil }
func (unsupportedRowsAffectedResult) RowsAffected() (int64, error) {
	return 0, driver.ErrSkip
}

func TestEnsureRowsAffected_IgnoresUnsupportedRowsAffected(t *testing.T) {
	if err := ensureRowsAffected(unsupportedRowsAffectedResult{}, 1, "unexpected rows"); err != nil {
		t.Fatalf("expected unsupported RowsAffected to be ignored, got %v", err)
	}
}

func TestEnsureRowsAffected_ReturnsConflictOnMismatch(t *testing.T) {
	err := ensureRowsAffected(driver.RowsAffected(0), 1, "unexpected rows")
	if err == nil {
		t.Fatalf("expected conflict")
	}
	if !errors.Is(err, errors.Conflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}
