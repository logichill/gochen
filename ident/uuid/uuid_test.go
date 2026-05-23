package uuid

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestGenerator_Next_Format 验证 Generator Next Format。
func TestGenerator_Next_Format(t *testing.T) {
	gen := NewGenerator()
	id, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(id) != 36 {
		t.Errorf("expected length 36, got %d", len(id))
	}

	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("invalid UUID format: %s", id)
	}

	if id[14] != '4' {
		t.Errorf("expected version 4, got %c", id[14])
	}
}

// TestGenerator_Next_Uniqueness 验证 Generator Next Uniqueness。
func TestGenerator_Next_Uniqueness(t *testing.T) {
	gen := NewGenerator()
	ids := make(map[string]bool)
	count := 10000

	for i := 0; i < count; i++ {
		id, err := gen.Next()
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
		if ids[id] {
			t.Fatalf("duplicate UUID at %d: %s", i, id)
		}
		ids[id] = true
	}
}

// TestGenerator_Next_Concurrent 验证 Generator Next Concurrent。
func TestGenerator_Next_Concurrent(t *testing.T) {
	gen := NewGenerator()
	var wg sync.WaitGroup
	ids := sync.Map{}
	count := 1000
	goroutines := 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < count; i++ {
				id, err := gen.Next()
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if _, loaded := ids.LoadOrStore(id, true); loaded {
					t.Errorf("duplicate UUID: %s", id)
				}
			}
		}()
	}
	wg.Wait()
}

// TestNew 验证 New。
func TestNew(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id) != 36 {
		t.Errorf("expected length 36, got %d", len(id))
	}
}

// TestParse 验证 Parse。
func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid with dashes", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid without dashes", "550e8400e29b41d4a716446655440000", false},
		{"invalid length", "550e8400", true},
		{"invalid hex", "gggggggg-gggg-gggg-gggg-gggggggggggg", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestIsValid 验证 IsValid。
func TestIsValid(t *testing.T) {
	if !IsValid("550e8400-e29b-41d4-a716-446655440000") {
		t.Error("expected valid UUID to return true")
	}
	if IsValid("invalid") {
		t.Error("expected invalid UUID to return false")
	}
}

// TestParse_RoundTrip 验证 Parse RoundTrip。
func TestParse_RoundTrip(t *testing.T) {
	id, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := Parse(id)
	if err != nil {
		t.Fatalf("failed to parse generated UUID: %v", err)
	}

	noHyphens := strings.ReplaceAll(id, "-", "")
	parsed2, err := Parse(noHyphens)
	if err != nil {
		t.Fatalf("failed to parse UUID without hyphens: %v", err)
	}

	if parsed != parsed2 {
		t.Error("parsed results should be equal")
	}
}

// TestNewV7 验证 NewV7。
func TestNewV7(t *testing.T) {
	id, err := NewV7()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(id) != 36 {
		t.Errorf("expected length 36, got %d", len(id))
	}

	if id[14] != '7' {
		t.Errorf("expected version 7, got %c", id[14])
	}
}

// TestNewV7_Ordering 验证 NewV7 Ordering。
func TestNewV7_Ordering(t *testing.T) {
	ids := make([]string, 100)
	for i := 0; i < 100; i++ {
		id, err := NewV7()
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
		ids[i] = id
	}

	for i := 1; i < len(ids); i++ {
		id1, _ := uuid.Parse(ids[i-1])
		id2, _ := uuid.Parse(ids[i])
		if id1.Time() > id2.Time() {
			t.Errorf("UUID v7 should be ordered: %s > %s", ids[i-1], ids[i])
		}
	}
}
