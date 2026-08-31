package assert

import (
	"errors"
	"testing"
	"time"
)

type mockT struct {
	failed bool
	msg    string
}

func (m *mockT) Errorf(format string, args ...any) {
	m.failed = true
	m.msg = format
}

func (m *mockT) FailNow() {
	m.failed = true
}

func (m *mockT) Helper() {}

func TestAssert_Basic(t *testing.T) {
	mock := &mockT{}

	// NoError
	if !NoError(mock, nil) || mock.failed {
		t.Error("NoError with nil should succeed")
	}
	mock.failed = false
	if NoError(mock, errors.New("err")) || !mock.failed {
		t.Error("NoError with err should fail")
	}

	// Equal
	mock.failed = false
	if !Equal(mock, 1, 1) || mock.failed {
		t.Error("Equal(1, 1) should succeed")
	}
	mock.failed = false
	if Equal(mock, 1, 2) || !mock.failed {
		t.Error("Equal(1, 2) should fail")
	}

	// True / False
	mock.failed = false
	if !True(mock, true) || mock.failed {
		t.Error("True(true) should succeed")
	}
	mock.failed = false
	if !False(mock, false) || mock.failed {
		t.Error("False(false) should succeed")
	}

	// Nil / NotNil
	mock.failed = false
	var ptr *int
	if !Nil(mock, ptr) || mock.failed {
		t.Error("Nil(typed nil) should succeed")
	}
	mock.failed = false
	val := 10
	if !NotNil(mock, &val) || mock.failed {
		t.Error("NotNil(&val) should succeed")
	}

	// Len / Empty / NotEmpty
	mock.failed = false
	slice := []int{1, 2, 3}
	if !Len(mock, slice, 3) || mock.failed {
		t.Error("Len(slice, 3) should succeed")
	}
	if !NotEmpty(mock, slice) || mock.failed {
		t.Error("NotEmpty(slice) should succeed")
	}
	if !Empty(mock, []int{}) || mock.failed {
		t.Error("Empty(empty slice) should succeed")
	}

	// Contains
	mock.failed = false
	if !Contains(mock, "hello world", "world") || mock.failed {
		t.Error("Contains string should succeed")
	}
	if !Contains(mock, []string{"a", "b"}, "a") || mock.failed {
		t.Error("Contains slice should succeed")
	}

	// Numbers & Durations
	mock.failed = false
	if !Greater(mock, 10, 5) || mock.failed {
		t.Error("Greater(10, 5) should succeed")
	}
	if !WithinDuration(mock, time.Now(), time.Now(), time.Second) || mock.failed {
		t.Error("WithinDuration should succeed")
	}

	// JSONEq
	mock.failed = false
	if !JSONEq(mock, `{"a":1,"b":2}`, `{"b":2,"a":1}`) || mock.failed {
		t.Error("JSONEq should succeed")
	}

	// Panics / NotPanics
	mock.failed = false
	if !Panics(mock, func() { panic("boom") }) || mock.failed {
		t.Error("Panics should succeed")
	}
	if !NotPanics(mock, func() {}) || mock.failed {
		t.Error("NotPanics should succeed")
	}
}
