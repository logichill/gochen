package outbox

import (
	"database/sql"

	"gochen/errors"
)

func ensureRowsAffected(result sql.Result, expected int64, message string) *errors.AppError {
	if result == nil {
		return nil
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil
	}
	if rows != expected {
		return errors.NewCode(errors.Conflict, message).
			WithContext("expected_rows", expected).
			WithContext("affected_rows", rows)
	}
	return nil
}
