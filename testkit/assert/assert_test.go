package assert

import (
	"errors"
	"math"
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
	if !Positive(mock, uint64(5)) || mock.failed {
		t.Error("Positive(uint64(5)) should succeed")
	}
	if !WithinDuration(mock, time.Now(), time.Now(), time.Second) || mock.failed {
		t.Error("WithinDuration should succeed")
	}

	// Equalf / NoErrorf formatting
	mock.failed = false
	if !Equalf(mock, "a", "a", "check %s", "value") || mock.failed {
		t.Error("Equalf should succeed")
	}
	if !NoErrorf(mock, nil, "no err %d", 1) || mock.failed {
		t.Error("NoErrorf should succeed")
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

	// NaN and EqualValues checks
	mock.failed = false
	if GreaterOrEqual(mock, math.NaN(), 1.0) || !mock.failed {
		t.Error("GreaterOrEqual with NaN should fail")
	}
	mock.failed = false
	if LessOrEqual(mock, math.NaN(), 1.0) || !mock.failed {
		t.Error("LessOrEqual with NaN should fail")
	}
	mock.failed = false
	if InDelta(mock, math.NaN(), 1.0, 0.1) || !mock.failed {
		t.Error("InDelta with NaN should fail")
	}
	mock.failed = false
	if EqualValues(mock, 1.5, 1) || !mock.failed {
		t.Error("EqualValues(1.5, 1) should fail")
	}
	mock.failed = false
	if !EqualValues(mock, 1.0, 1) || mock.failed {
		t.Error("EqualValues(1.0, 1) should succeed")
	}
	mock.failed = false
	if !EqualValues(mock, int32(5), int64(5)) || mock.failed {
		t.Error("EqualValues(int32(5), int64(5)) should succeed")
	}
}

func TestEqualValues_RejectsCompensatingAndOneWayConversions(t *testing.T) {
	mock := &mockT{}
	expectFail := func(name string, expected, actual any) {
		mock.failed = false
		if EqualValues(mock, expected, actual) || !mock.failed {
			t.Errorf("EqualValues(%s) should fail", name)
		}
	}
	expectPass := func(name string, expected, actual any) {
		mock.failed = false
		if !EqualValues(mock, expected, actual) || mock.failed {
			t.Errorf("EqualValues(%s) should succeed", name)
		}
	}

	expectFail("int64(-1), uint64(MaxUint64)", int64(-1), uint64(math.MaxUint64))
	expectFail("uint64(MaxUint64), int64(-1)", uint64(math.MaxUint64), int64(-1))
	expectFail("uint8(255), int8(-1)", uint8(255), int8(-1))
	expectFail("int8(-1), uint8(255)", int8(-1), uint8(255))
	expectFail("65, \"A\"", 65, "A")
	expectFail("\"A\", 65", "A", 65)
	expectFail("1, \"1\"", 1, "1")
	expectFail("2^53+1, float64(2^53)", int64(9007199254740993), float64(9007199254740992))
	expectFail("float64(2^53), 2^53+1", float64(9007199254740992), int64(9007199254740993))

	expectPass("int32(5), int64(5)", int32(5), int64(5))
	expectPass("int32(-5), int64(-5)", int32(-5), int64(-5))
	expectPass("int(7), uint64(7)", 7, uint64(7))
	expectPass("1.0, 1", 1.0, 1)
	expectPass("uint64(2), 2.0", uint64(2), 2.0)
	expectPass("float32(1.5), float64(1.5)", float32(1.5), float64(1.5))
	expectPass("\"ab\", []byte(\"ab\")", "ab", []byte("ab"))
}

type customAppErr struct{ code string }

func (e *customAppErr) Error() string { return "custom:" + e.code }

func TestAssert_AllHelpersAndFailureBranches(t *testing.T) {
	mock := &mockT{}
	check := func(name string, want bool, got bool) {
		t.Helper()
		if got != want || mock.failed != !want {
			t.Fatalf("%s: got=%v failed=%v, want=%v", name, got, mock.failed, want)
		}
		mock.failed = false
	}

	// formatMsg branches (empty, single string, format+args via *f, non-string)
	check("Equal with non-string msg", false, Equal(mock, 1, 2, 123))

	// Equal / NotEqual / Equalf / NotEqualf / EqualValues / EqualValuesf
	check("Equal(nil, nil)", true, Equal(mock, nil, nil))
	check("Equal(nil, 1)", false, Equal(mock, nil, 1))
	check("NotEqual(1, 2)", true, NotEqual(mock, 1, 2))
	check("NotEqual(nil, nil)", false, NotEqual(mock, nil, nil))
	check("NotEqual(1, 1)", false, NotEqual(mock, 1, 1))
	check("NotEqualf(1, 2)", true, NotEqualf(mock, 1, 2, "ok %d", 1))
	check("NotEqualf(1, 1)", false, NotEqualf(mock, 1, 1, "bad %d", 1))
	check("EqualValues(nil, nil)", true, EqualValues(mock, nil, nil))
	check("EqualValues(nil, 1)", false, EqualValues(mock, nil, 1))
	check("EqualValues(struct, struct)", false, EqualValues(mock, struct{ A int }{1}, struct{ B string }{"x"}))
	check("EqualValues(float -int64)", true, EqualValues(mock, float64(-3), int64(-3)))
	check("EqualValues(float -1e30, int64)", false, EqualValues(mock, -1e30, int64(-1)))
	check("EqualValues(float 1e30, int64)", false, EqualValues(mock, 1e30, int64(1)))
	check("EqualValues(float +5, int64(-5))", false, EqualValues(mock, float64(5), int64(-5)))
	check("EqualValues(float -1, uint64)", false, EqualValues(mock, float64(-1), uint64(1)))
	check("EqualValues(uint32, uint64)", true, EqualValues(mock, uint32(9), uint64(9)))
	check("EqualValuesf pass", true, EqualValuesf(mock, int32(3), int64(3), "msg %d", 1))
	check("EqualValuesf fail", false, EqualValuesf(mock, int32(3), int64(4), "msg %d", 1))

	// Same / NotSame / IsType
	x, y := 1, 1
	check("Same(&x, &x)", true, Same(mock, &x, &x))
	check("Same(&x, &y)", false, Same(mock, &x, &y))
	check("Same(nil, &x)", false, Same(mock, nil, &x))
	check("Same(non-ptr, non-ptr)", false, Same(mock, 1, 1))
	check("NotSame(&x, &y)", true, NotSame(mock, &x, &y))
	check("NotSame(&x, &x)", false, NotSame(mock, &x, &x))
	check("NotSame(non-ptr, &x)", false, NotSame(mock, 1, &x))
	check("IsType match", true, IsType(mock, 0, 42))
	check("IsType mismatch", false, IsType(mock, 0, "42"))

	// Collection: Len / Lenf / Contains / Containsf / NotContains / NotContainsf / JSONEq / ElementsMatch
	check("Len(nil)", false, Len(mock, nil, 0))
	check("Len(mismatch)", false, Len(mock, []int{1}, 2))
	check("Len(non-collection)", false, Len(mock, 123, 1))
	check("Lenf pass", true, Lenf(mock, "abc", 3, "len %d", 3))
	check("Lenf fail", false, Lenf(mock, "abc", 2, "len %d", 2))
	check("Contains string fail", false, Contains(mock, "abc", "z"))
	check("Contains slice fail", false, Contains(mock, []int{1, 2}, 9))
	check("Contains map pass", true, Contains(mock, map[string]int{"k": 1}, "k"))
	check("Contains map fail", false, Contains(mock, map[string]int{"k": 1}, "missing"))
	check("Contains unsupported", false, Contains(mock, 123, 1))
	check("Containsf pass", true, Containsf(mock, "hello", "ell", "msg %s", "x"))
	check("NotContains string pass", true, NotContains(mock, "abc", "z"))
	check("NotContains string fail", false, NotContains(mock, "abc", "b"))
	check("NotContains slice pass", true, NotContains(mock, []int{1, 2}, 9))
	check("NotContains slice fail", false, NotContains(mock, []int{1, 2}, 2))
	check("NotContains map pass", true, NotContains(mock, map[string]int{"k": 1}, "x"))
	check("NotContains map fail", false, NotContains(mock, map[string]int{"k": 1}, "k"))
	check("NotContains non-collection", true, NotContains(mock, 123, 1))
	check("NotContainsf pass", true, NotContainsf(mock, "abc", "z", "msg %s", "x"))
	check("JSONEq invalid expected", false, JSONEq(mock, "{bad", `{}`))
	check("JSONEq invalid actual", false, JSONEq(mock, `{}`, "{bad"))
	check("JSONEq mismatch", false, JSONEq(mock, `{"a":1}`, `{"a":2}`))
	check("ElementsMatch pass", true, ElementsMatch(mock, []int{1, 2, 2}, []int{2, 1, 2}))
	check("ElementsMatch non-slice", false, ElementsMatch(mock, 123, []int{1}))
	check("ElementsMatch len mismatch", false, ElementsMatch(mock, []int{1}, []int{1, 2}))
	check("ElementsMatch content mismatch", false, ElementsMatch(mock, []int{1, 1}, []int{1, 2}))

	// Compare: Greater / GreaterOrEqual / Less / LessOrEqual / Positive / WithinDuration / InDelta
	now := time.Now()
	check("Greater fail", false, Greater(mock, 1, 2))
	check("GreaterOrEqual pass", true, GreaterOrEqual(mock, 2, 2))
	check("Less pass", true, Less(mock, 1, 2))
	check("Less fail", false, Less(mock, 2, 1))
	check("LessOrEqual pass", true, LessOrEqual(mock, 2, 2))
	check("Compare uint vs int", true, Greater(mock, uint(5), -1))
	check("Compare uint vs pos int", true, Less(mock, uint(2), 5))
	check("Compare int vs uint", true, Less(mock, -1, uint(5)))
	check("Compare pos int vs uint", true, Greater(mock, 5, uint(2)))
	check("Compare float vs int", true, Greater(mock, 3.5, 3))
	check("Compare float vs uint", true, Less(mock, 1.5, uint(2)))
	check("Compare time.Time", true, Less(mock, now, now.Add(time.Second)))
	check("Compare time.Time equal", true, LessOrEqual(mock, now, now))
	check("Compare time.Time after", true, Greater(mock, now.Add(time.Second), now))
	check("Compare non-numeric", false, Greater(mock, "a", "b"))
	check("Positive fail", false, Positive(mock, 0))
	check("WithinDuration reverse order", true, WithinDuration(mock, now, now.Add(10*time.Millisecond), 50*time.Millisecond))
	check("WithinDuration fail", false, WithinDuration(mock, now, now.Add(time.Second), 10*time.Millisecond))
	check("InDelta pass", true, InDelta(mock, 10, uint(11), 1.5))
	check("InDelta fail", false, InDelta(mock, 10.0, 12.0, 0.5))
	check("InDelta non-number", false, InDelta(mock, "a", 1.0, 0.5))

	// Error: Error / Errorf / ErrorIs / ErrorIsf / ErrorAs / ErrorContains / ErrorContainsf
	baseErr := &customAppErr{code: "E1"}
	wrapped := errors.Join(baseErr, AnError)
	check("Error pass", true, Error(mock, wrapped))
	check("Error fail", false, Error(mock, nil))
	check("Errorf pass", true, Errorf(mock, wrapped, "err %d", 1))
	check("ErrorIs pass", true, ErrorIs(mock, wrapped, AnError))
	check("ErrorIs fail", false, ErrorIs(mock, wrapped, errors.New("other")))
	check("ErrorIsf pass", true, ErrorIsf(mock, wrapped, AnError, "is %s", "anErr"))
	var target *customAppErr
	check("ErrorAs pass", true, ErrorAs(mock, wrapped, &target))
	check("ErrorAs nil", false, ErrorAs(mock, nil, &target))
	var wrongTarget *time.ParseError
	check("ErrorAs mismatch", false, ErrorAs(mock, wrapped, &wrongTarget))
	check("ErrorContains pass", true, ErrorContains(mock, wrapped, "custom:E1"))
	check("ErrorContains nil", false, ErrorContains(mock, nil, "x"))
	check("ErrorContains mismatch", false, ErrorContains(mock, wrapped, "not-found"))
	check("ErrorContainsf pass", true, ErrorContainsf(mock, wrapped, "E1", "fmt %d", 1))

	// Execution: NotPanics / Panics / PanicsWithValue / Eventually
	NotPanics(mock, func() { panic("unexpected") })
	if !mock.failed {
		t.Fatal("NotPanics on panicking func must mark failed")
	}
	mock.failed = false
	check("Panics fail", false, Panics(mock, func() {}))
	check("PanicsWithValue pass", true, PanicsWithValue(mock, "boom", func() { panic("boom") }))
	check("PanicsWithValue no panic", false, PanicsWithValue(mock, "boom", func() {}))
	check("PanicsWithValue wrong val", false, PanicsWithValue(mock, "boom", func() { panic("other") }))
	check("Eventually pass", true, Eventually(mock, func() bool { return true }, 20*time.Millisecond, time.Millisecond))
	check("Eventually fail", false, Eventually(mock, func() bool { return false }, 5*time.Millisecond, time.Millisecond))

	// Value: True/False/Nil/NotNil/Empty/NotEmpty/Zero/NotZero + *f variants
	check("True fail", false, True(mock, false))
	check("Truef pass", true, Truef(mock, true, "t %d", 1))
	check("False fail", false, False(mock, true))
	check("Falsef pass", true, Falsef(mock, false, "f %d", 1))
	check("Nil fail", false, Nil(mock, 123))
	check("Nilf pass", true, Nilf(mock, nil, "n %d", 1))
	check("NotNil fail", false, NotNil(mock, nil))
	check("NotNilf pass", true, NotNilf(mock, 1, "nn %d", 1))
	check("Empty nil", true, Empty(mock, nil))
	check("Empty zero int", true, Empty(mock, 0))
	check("Empty fail", false, Empty(mock, "non-empty"))
	check("Emptyf pass", true, Emptyf(mock, "", "e %d", 1))
	check("NotEmpty fail", false, NotEmpty(mock, ""))
	check("NotEmptyf pass", true, NotEmptyf(mock, "x", "ne %d", 1))
	check("Zero nil", true, Zero(mock, nil))
	check("Zero 0", true, Zero(mock, 0))
	check("Zero fail", false, Zero(mock, 1))
	check("Zerof pass", true, Zerof(mock, 0, "z %d", 1))
	check("NotZero pass", true, NotZero(mock, 1))
	check("NotZero fail", false, NotZero(mock, 0))
	check("NotZerof pass", true, NotZerof(mock, 1, "nz %d", 1))
}
