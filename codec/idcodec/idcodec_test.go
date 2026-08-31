package idcodec

import (
	"math"
	"strconv"
	"testing"

	"gochen/errors"
)

func TestInt64_Decode_TrimsSpace(t *testing.T) {
	c := NewInt64[int64]()

	got, err := c.Decode(" 123 \n")
	if err != nil {
		t.Fatalf("Decode(string) returned error: %v", err)
	}
	if got != 123 {
		t.Fatalf("unexpected value: want=%d got=%d", 123, got)
	}

	got, err = c.Decode([]byte("\t456 \n"))
	if err != nil {
		t.Fatalf("Decode([]byte) returned error: %v", err)
	}
	if got != 456 {
		t.Fatalf("unexpected value: want=%d got=%d", 456, got)
	}
}

func TestInt64_Decode_UintOverflow(t *testing.T) {
	c := NewInt64[int64]()

	if _, err := c.Decode(uint64(math.MaxInt64) + 1); err == nil {
		t.Fatalf("expected overflow error, got nil")
	} else if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}

	if got, err := c.Decode(uint64(math.MaxInt64)); err != nil {
		t.Fatalf("Decode(max int64 as uint64) returned error: %v", err)
	} else if got != math.MaxInt64 {
		t.Fatalf("unexpected value: want=%d got=%d", int64(math.MaxInt64), got)
	}

	if strconv.IntSize == 64 {
		max := uint64(math.MaxInt64)
		if _, err := c.Decode(uint(max + 1)); err == nil {
			t.Fatalf("expected overflow error for uint, got nil")
		} else if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("expected InvalidInput for uint overflow, got: %v", err)
		}
	}
}

func TestInt64_Decode_EmptyAfterTrim(t *testing.T) {
	c := NewInt64[int64]()

	if _, err := c.Decode(" \n\t "); err == nil {
		t.Fatalf("expected error for empty string after trim, got nil")
	}
	if _, err := c.Decode([]byte(" \n\t ")); err == nil {
		t.Fatalf("expected error for empty bytes after trim, got nil")
	}
}
