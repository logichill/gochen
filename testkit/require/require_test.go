package require

import (
	"errors"
	"testing"
	"time"
)

// mockT 记录 Errorf/FailNow/Helper 调用，用于验证 require 的 FailNow 阻断语义。
type mockT struct {
	failed  bool
	failNow bool
	msg     string
	helperN int
}

func (m *mockT) Errorf(format string, args ...any) {
	m.failed = true
	m.msg = format
}

func (m *mockT) FailNow() {
	m.failNow = true
}

func (m *mockT) Helper() { m.helperN++ }

func TestRequire_Equal(t *testing.T) {
	m := &mockT{}
	Equal(m, 1, 1)
	if m.failed || m.failNow {
		t.Fatalf("Equal on equal values should pass: failed=%v failNow=%v msg=%s", m.failed, m.failNow, m.msg)
	}

	m = &mockT{}
	Equal(m, 1, 2)
	if !m.failNow {
		t.Fatalf("Equal on unequal values must call FailNow")
	}
}

func TestRequire_NoError(t *testing.T) {
	m := &mockT{}
	NoError(m, nil)
	if m.failNow {
		t.Fatal("NoError(nil) must not FailNow")
	}

	m = &mockT{}
	NoError(m, errors.New("boom"))
	if !m.failNow {
		t.Fatal("NoError(err) must FailNow")
	}
}

func TestRequire_Error(t *testing.T) {
	m := &mockT{}
	Error(m, errors.New("boom"))
	if m.failNow {
		t.Fatal("Error(err) must not FailNow")
	}

	m = &mockT{}
	Error(m, nil)
	if !m.failNow {
		t.Fatal("Error(nil) must FailNow")
	}
}

func TestRequire_NilNotNil(t *testing.T) {
	m := &mockT{}
	Nil(m, nil)
	if m.failNow {
		t.Fatal("Nil(nil) must not FailNow")
	}

	m = &mockT{}
	NotNil(m, nil)
	if !m.failNow {
		t.Fatal("NotNil(nil) must FailNow")
	}
}

func TestRequire_TrueFalse(t *testing.T) {
	m := &mockT{}
	True(m, true)
	if m.failNow {
		t.Fatal("True(true) must not FailNow")
	}
	m = &mockT{}
	False(m, true)
	if !m.failNow {
		t.Fatal("False(true) must FailNow")
	}
}

func TestRequire_Panics(t *testing.T) {
	m := &mockT{}
	Panics(m, func() { panic("x") })
	if m.failNow {
		t.Fatal("Panics on panicking func must not FailNow")
	}

	m = &mockT{}
	Panics(m, func() {})
	if !m.failNow {
		t.Fatal("Panics on non-panicking func must FailNow")
	}
}

func TestRequire_Eventually(t *testing.T) {
	m := &mockT{}
	Eventually(m, func() bool { return true }, 50*time.Millisecond, time.Millisecond)
	if m.failNow {
		t.Fatal("Eventually on immediate true must not FailNow")
	}

	m = &mockT{}
	count := 0
	Eventually(m, func() bool { count++; return count > 3 }, 50*time.Millisecond, time.Millisecond)
	if m.failNow {
		t.Fatal("Eventually must pass once condition becomes true")
	}

	m = &mockT{}
	Eventually(m, func() bool { return false }, 10*time.Millisecond, time.Millisecond)
	if !m.failNow {
		t.Fatal("Eventually must FailNow when condition never satisfied")
	}
}

func TestRequire_FailNowMessagePropagation(t *testing.T) {
	m := &mockT{}
	Equal(m, "a", "b", "custom message")
	if !m.failNow {
		t.Fatal("expected FailNow")
	}
	if m.msg == "" {
		t.Fatal("expected error message propagation")
	}
}

type customRequireErr struct{}

func (customRequireErr) Error() string { return "custom require error" }

func TestRequire_AllWrappersPassAndFailNow(t *testing.T) {
	assertPassAndFail := func(name string, passFn func(TestingT), failFn func(TestingT)) {
		t.Helper()
		m := &mockT{}
		passFn(m)
		if m.failNow || m.failed {
			t.Fatalf("%s pass case unexpectedly failed: failNow=%v failed=%v", name, m.failNow, m.failed)
		}
		m = &mockT{}
		failFn(m)
		if !m.failNow {
			t.Fatalf("%s fail case did not call FailNow", name)
		}
	}

	x, y := 1, 1
	now := time.Now()
	targetErr := customRequireErr{}

	// Equal family
	assertPassAndFail("Equalf", func(m TestingT) { Equalf(m, 1, 1, "eq %d", 1) }, func(m TestingT) { Equalf(m, 1, 2, "eq %d", 1) })
	assertPassAndFail("NotEqual", func(m TestingT) { NotEqual(m, 1, 2) }, func(m TestingT) { NotEqual(m, 1, 1) })
	assertPassAndFail("NotEqualf", func(m TestingT) { NotEqualf(m, 1, 2, "ne %d", 1) }, func(m TestingT) { NotEqualf(m, 1, 1, "ne %d", 1) })
	assertPassAndFail("EqualValues", func(m TestingT) { EqualValues(m, int32(2), int64(2)) }, func(m TestingT) { EqualValues(m, int32(2), int64(3)) })
	assertPassAndFail("EqualValuesf", func(m TestingT) { EqualValuesf(m, int32(2), int64(2), "ev %d", 1) }, func(m TestingT) { EqualValuesf(m, int32(2), int64(3), "ev %d", 1) })
	assertPassAndFail("Same", func(m TestingT) { Same(m, &x, &x) }, func(m TestingT) { Same(m, &x, &y) })
	assertPassAndFail("NotSame", func(m TestingT) { NotSame(m, &x, &y) }, func(m TestingT) { NotSame(m, &x, &x) })
	assertPassAndFail("IsType", func(m TestingT) { IsType(m, 0, 1) }, func(m TestingT) { IsType(m, 0, "1") })

	// Collection family
	assertPassAndFail("Len", func(m TestingT) { Len(m, []int{1, 2}, 2) }, func(m TestingT) { Len(m, []int{1}, 2) })
	assertPassAndFail("Lenf", func(m TestingT) { Lenf(m, []int{1, 2}, 2, "l %d", 2) }, func(m TestingT) { Lenf(m, []int{1}, 2, "l %d", 2) })
	assertPassAndFail("Contains", func(m TestingT) { Contains(m, "abc", "b") }, func(m TestingT) { Contains(m, "abc", "z") })
	assertPassAndFail("Containsf", func(m TestingT) { Containsf(m, "abc", "b", "c %s", "b") }, func(m TestingT) { Containsf(m, "abc", "z", "c %s", "z") })
	assertPassAndFail("NotContains", func(m TestingT) { NotContains(m, "abc", "z") }, func(m TestingT) { NotContains(m, "abc", "b") })
	assertPassAndFail("NotContainsf", func(m TestingT) { NotContainsf(m, "abc", "z", "nc %s", "z") }, func(m TestingT) { NotContainsf(m, "abc", "b", "nc %s", "b") })
	assertPassAndFail("JSONEq", func(m TestingT) { JSONEq(m, `{"a":1}`, `{"a":1}`) }, func(m TestingT) { JSONEq(m, `{"a":1}`, `{"a":2}`) })
	assertPassAndFail("ElementsMatch", func(m TestingT) { ElementsMatch(m, []int{1, 2}, []int{2, 1}) }, func(m TestingT) { ElementsMatch(m, []int{1, 2}, []int{1, 3}) })

	// Compare family
	assertPassAndFail("Greater", func(m TestingT) { Greater(m, 2, 1) }, func(m TestingT) { Greater(m, 1, 2) })
	assertPassAndFail("GreaterOrEqual", func(m TestingT) { GreaterOrEqual(m, 2, 2) }, func(m TestingT) { GreaterOrEqual(m, 1, 2) })
	assertPassAndFail("Less", func(m TestingT) { Less(m, 1, 2) }, func(m TestingT) { Less(m, 2, 1) })
	assertPassAndFail("LessOrEqual", func(m TestingT) { LessOrEqual(m, 2, 2) }, func(m TestingT) { LessOrEqual(m, 2, 1) })
	assertPassAndFail("Positive", func(m TestingT) { Positive(m, 1) }, func(m TestingT) { Positive(m, 0) })
	assertPassAndFail("WithinDuration", func(m TestingT) { WithinDuration(m, now, now, time.Second) }, func(m TestingT) { WithinDuration(m, now, now.Add(time.Minute), time.Second) })
	assertPassAndFail("InDelta", func(m TestingT) { InDelta(m, 1.0, 1.1, 0.2) }, func(m TestingT) { InDelta(m, 1.0, 2.0, 0.2) })

	// Error family
	assertPassAndFail("NoErrorf", func(m TestingT) { NoErrorf(m, nil, "ok %d", 1) }, func(m TestingT) { NoErrorf(m, AnError, "err %d", 1) })
	assertPassAndFail("Errorf", func(m TestingT) { Errorf(m, AnError, "err %d", 1) }, func(m TestingT) { Errorf(m, nil, "err %d", 1) })
	assertPassAndFail("ErrorIs", func(m TestingT) { ErrorIs(m, AnError, AnError) }, func(m TestingT) { ErrorIs(m, AnError, errors.New("x")) })
	assertPassAndFail("ErrorIsf", func(m TestingT) { ErrorIsf(m, AnError, AnError, "is %d", 1) }, func(m TestingT) { ErrorIsf(m, AnError, errors.New("x"), "is %d", 1) })
	assertPassAndFail("ErrorAs", func(m TestingT) {
		var dst customRequireErr
		ErrorAs(m, targetErr, &dst)
	}, func(m TestingT) {
		var dst customRequireErr
		ErrorAs(m, nil, &dst)
	})
	assertPassAndFail("ErrorContains", func(m TestingT) { ErrorContains(m, targetErr, "custom") }, func(m TestingT) { ErrorContains(m, targetErr, "missing") })
	assertPassAndFail("ErrorContainsf", func(m TestingT) { ErrorContainsf(m, targetErr, "custom", "c %d", 1) }, func(m TestingT) { ErrorContainsf(m, targetErr, "missing", "c %d", 1) })

	// Execution family
	assertPassAndFail("NotPanics", func(m TestingT) { NotPanics(m, func() {}) }, func(m TestingT) { NotPanics(m, func() { panic("boom") }) })
	assertPassAndFail("PanicsWithValue", func(m TestingT) { PanicsWithValue(m, "boom", func() { panic("boom") }) }, func(m TestingT) { PanicsWithValue(m, "boom", func() { panic("other") }) })

	// Value family
	assertPassAndFail("True", func(m TestingT) { True(m, true) }, func(m TestingT) { True(m, false) })
	assertPassAndFail("Truef", func(m TestingT) { Truef(m, true, "t %d", 1) }, func(m TestingT) { Truef(m, false, "t %d", 1) })
	assertPassAndFail("False", func(m TestingT) { False(m, false) }, func(m TestingT) { False(m, true) })
	assertPassAndFail("Falsef", func(m TestingT) { Falsef(m, false, "f %d", 1) }, func(m TestingT) { Falsef(m, true, "f %d", 1) })
	assertPassAndFail("Nil", func(m TestingT) { Nil(m, nil) }, func(m TestingT) { Nil(m, 1) })
	assertPassAndFail("Nilf", func(m TestingT) { Nilf(m, nil, "n %d", 1) }, func(m TestingT) { Nilf(m, 1, "n %d", 1) })
	assertPassAndFail("NotNil", func(m TestingT) { NotNil(m, 1) }, func(m TestingT) { NotNil(m, nil) })
	assertPassAndFail("NotNilf", func(m TestingT) { NotNilf(m, 1, "nn %d", 1) }, func(m TestingT) { NotNilf(m, nil, "nn %d", 1) })
	assertPassAndFail("Empty", func(m TestingT) { Empty(m, "") }, func(m TestingT) { Empty(m, "x") })
	assertPassAndFail("Emptyf", func(m TestingT) { Emptyf(m, "", "e %d", 1) }, func(m TestingT) { Emptyf(m, "x", "e %d", 1) })
	assertPassAndFail("NotEmpty", func(m TestingT) { NotEmpty(m, "x") }, func(m TestingT) { NotEmpty(m, "") })
	assertPassAndFail("NotEmptyf", func(m TestingT) { NotEmptyf(m, "x", "ne %d", 1) }, func(m TestingT) { NotEmptyf(m, "", "ne %d", 1) })
	assertPassAndFail("Zero", func(m TestingT) { Zero(m, 0) }, func(m TestingT) { Zero(m, 1) })
	assertPassAndFail("Zerof", func(m TestingT) { Zerof(m, 0, "z %d", 1) }, func(m TestingT) { Zerof(m, 1, "z %d", 1) })
	assertPassAndFail("NotZero", func(m TestingT) { NotZero(m, 1) }, func(m TestingT) { NotZero(m, 0) })
	assertPassAndFail("NotZerof", func(m TestingT) { NotZerof(m, 1, "nz %d", 1) }, func(m TestingT) { NotZerof(m, 0, "nz %d", 1) })
}
