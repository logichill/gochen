package db

import (
	"errors"
	"fmt"
	"testing"
)

type sqliteLikeError struct {
	Code         int64
	ExtendedCode int64
	msg          string
}

func (e sqliteLikeError) Error() string { return e.msg }

type codeMethodError struct{ code int }

func (e codeMethodError) Error() string { return "boom" }
func (e codeMethodError) Code() int     { return e.code }

type mysqlNumberError struct{ Number uint16 }

func (e mysqlNumberError) Error() string { return "mysql error" }

type postgresStateMethodError struct{ state string }

func (e postgresStateMethodError) Error() string    { return "postgres error" }
func (e postgresStateMethodError) SQLState() string { return e.state }

type postgresStateFieldError struct{ SQLState string }

func (e postgresStateFieldError) Error() string { return "postgres error" }

func TestIsUniqueViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"sqlite extended code 2067", sqliteLikeError{Code: 19, ExtendedCode: 2067, msg: "constraint failed"}, true},
		{"sqlite primary key 1555", sqliteLikeError{Code: 19, ExtendedCode: 1555, msg: "constraint failed"}, true},
		{"sqlite base constraint 19 not unique", sqliteLikeError{Code: 19, msg: "constraint failed"}, false},
		{"sqlite not null 1299 not unique", sqliteLikeError{Code: 19, ExtendedCode: 1299, msg: "NOT NULL constraint failed"}, false},
		{"sqlite foreign key 787 not unique", sqliteLikeError{Code: 19, ExtendedCode: 787, msg: "FOREIGN KEY constraint failed"}, false},
		{"sqlite check 275 not unique", sqliteLikeError{Code: 19, ExtendedCode: 275, msg: "CHECK constraint failed"}, false},
		{"non-unique code 5", sqliteLikeError{Code: 5, msg: "busy"}, false},
		{"code method unique", codeMethodError{code: 2067}, true},
		{"code method non-unique", codeMethodError{code: 5}, false},
		{"mysql number 1062", mysqlNumberError{Number: 1062}, true},
		{"mysql number non-unique", mysqlNumberError{Number: 1452}, false},
		{"postgres sqlstate method 23505", postgresStateMethodError{state: "23505"}, true},
		{"postgres sqlstate field 23505", postgresStateFieldError{SQLState: "23505"}, true},
		{"postgres sqlstate non-unique", postgresStateMethodError{state: "23503"}, false},
		{"mysql text", errors.New("Error 1062: Duplicate entry 'x' for key 'PRIMARY'"), true},
		{"postgres sqlstate text", errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)"), true},
		{"mysql sqlstate 23000 not unique", errors.New("Error 1452 (SQLSTATE 23000): Cannot add or update a child row: a foreign key constraint fails"), false},
		{"sqlite text", errors.New("UNIQUE constraint failed: events.id"), true},
		{"unrelated", errors.New("connection refused"), false},
		{"wrapped structured", fmt.Errorf("append failed: %w", sqliteLikeError{ExtendedCode: 2067, msg: "x"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
