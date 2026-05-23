package datecode

import (
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestGenerator_Next_DefaultFormat 验证 Generator Next DefaultFormat。
func TestGenerator_Next_DefaultFormat(t *testing.T) {
	gen := NewGenerator()
	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 默认格式：YYYYMMDD + 6 位数字
	if len(code) != 14 {
		t.Errorf("expected length 14, got %d: %s", len(code), code)
	}

	// 检查日期部分
	today := time.Now().Format("20060102")
	if !strings.HasPrefix(code, today) {
		t.Errorf("expected prefix %s, got %s", today, code)
	}
}

// TestGenerator_Next_WithPrefix 验证 Generator Next WithPrefix。
func TestGenerator_Next_WithPrefix(t *testing.T) {
	gen := NewGenerator(
		WithPrefix("ORD"),
		WithSuffixDigits(4),
	)
	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(code, "ORD") {
		t.Errorf("expected prefix ORD, got %s", code)
	}

	// ORD + YYYYMMDD + 4 位 = 3 + 8 + 4 = 15
	if len(code) != 15 {
		t.Errorf("expected length 15, got %d: %s", len(code), code)
	}
}

// TestGenerator_Next_WithSeparator 验证 Generator Next WithSeparator。
func TestGenerator_Next_WithSeparator(t *testing.T) {
	gen := NewGenerator(
		WithPrefix("TX"),
		WithSeparator("-"),
		WithSuffixDigits(4),
	)
	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// TX + YYYYMMDD + - + 4 位 = 2 + 8 + 1 + 4 = 15
	if len(code) != 15 {
		t.Errorf("expected length 15, got %d: %s", len(code), code)
	}

	// 检查分隔符
	parts := strings.Split(code, "-")
	if len(parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(parts))
	}
}

// TestGenerator_Next_SequenceMode 验证 Generator Next SequenceMode。
func TestGenerator_Next_SequenceMode(t *testing.T) {
	gen := NewGenerator(
		WithSuffixType(SuffixSequence),
		WithSuffixDigits(4),
	)

	codes := make([]string, 5)
	for i := 0; i < 5; i++ {
		code, err := gen.Next()
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
		codes[i] = code
	}

	// 检查序列号递增
	for i := 0; i < 5; i++ {
		expected := time.Now().Format("20060102") + strings.Repeat("0", 3) + string('1'+byte(i))
		if codes[i] != expected {
			t.Errorf("expected %s, got %s", expected, codes[i])
		}
	}
}

// TestGenerator_Next_SequenceResetOnNewDay 验证 Generator Next SequenceResetOnNewDay。
func TestGenerator_Next_SequenceResetOnNewDay(t *testing.T) {
	gen := NewGenerator(
		WithSuffixType(SuffixSequence),
		WithSuffixDigits(4),
	)

	// 模拟不同日期
	gen.lastDate = "20250113"
	gen.sequence = 100

	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 应该重置为 0001
	today := time.Now().Format("20060102")
	if !strings.HasSuffix(code, "0001") {
		t.Errorf("expected sequence reset to 0001, got %s", code)
	}
	if !strings.HasPrefix(code, today) {
		t.Errorf("expected prefix %s, got %s", today, code)
	}
}

// TestGenerator_Next_DifferentFormats 验证 Generator Next DifferentFormats。
func TestGenerator_Next_DifferentFormats(t *testing.T) {
	tests := []struct {
		format   Format
		expected int // 日期部分长度
	}{
		{FormatYYYYMMDD, 8},
		{FormatYYYYMMDDHH, 10},
		{FormatYYYYMMDDHHMM, 12},
		{FormatYYYYMMDDHHMMSS, 14},
		{FormatYYMMDD, 6},
		{FormatMMDD, 4},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			gen := NewGenerator(
				WithFormat(tt.format),
				WithSuffixDigits(4),
			)
			code, err := gen.Next()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expectedLen := tt.expected + 4
			if len(code) != expectedLen {
				t.Errorf("expected length %d, got %d: %s", expectedLen, len(code), code)
			}
		})
	}
}

// TestGenerator_Next_Concurrent 验证 Generator Next Concurrent。
func TestGenerator_Next_Concurrent(t *testing.T) {
	gen := NewGenerator(
		WithSuffixType(SuffixRandom),
		WithSuffixDigits(8),
	)

	var wg sync.WaitGroup
	codes := sync.Map{}
	count := 1000
	goroutines := 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < count; i++ {
				code, err := gen.Next()
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if _, loaded := codes.LoadOrStore(code, true); loaded {
					// 随机模式下小概率重复，不算错误
					t.Logf("duplicate code (acceptable in random mode): %s", code)
				}
			}
		}()
	}
	wg.Wait()
}

// TestNewOrderCodeGenerator 验证 NewOrderCodeGenerator。
func TestNewOrderCodeGenerator(t *testing.T) {
	gen := NewOrderCodeGenerator()
	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ORD + YYYYMMDD + 8 位 = 3 + 8 + 8 = 19
	if len(code) != 19 {
		t.Errorf("expected length 19, got %d: %s", len(code), code)
	}
	if !strings.HasPrefix(code, "ORD") {
		t.Errorf("expected prefix ORD, got %s", code)
	}
}

// TestNewTransactionCodeGenerator 验证 NewTransactionCodeGenerator。
func TestNewTransactionCodeGenerator(t *testing.T) {
	gen := NewTransactionCodeGenerator()
	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// TXN + YYYYMMDDHHMMss + 6 位 = 3 + 14 + 6 = 23
	if len(code) != 23 {
		t.Errorf("expected length 23, got %d: %s", len(code), code)
	}
	if !strings.HasPrefix(code, "TXN") {
		t.Errorf("expected prefix TXN, got %s", code)
	}
}

// TestNewSerialCodeGenerator 验证 NewSerialCodeGenerator。
func TestNewSerialCodeGenerator(t *testing.T) {
	gen := NewSerialCodeGenerator("INV", 5)
	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// INV + YYYYMMDD + 5 位 = 3 + 8 + 5 = 16
	if len(code) != 16 {
		t.Errorf("expected length 16, got %d: %s", len(code), code)
	}
	if !strings.HasPrefix(code, "INV") {
		t.Errorf("expected prefix INV, got %s", code)
	}
}

// TestGenerator_SequenceOverflow 验证 Generator SequenceOverflow。
func TestGenerator_SequenceOverflow(t *testing.T) {
	gen := NewGenerator(
		WithSuffixType(SuffixSequence),
		WithSuffixDigits(2), // 最大 99
	)

	today := time.Now().Format("20060102")
	gen.lastDate = today
	gen.sequence = 99

	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 溢出后应该回到 01
	if !strings.HasSuffix(code, "01") {
		t.Errorf("expected sequence wrap to 01, got %s", code)
	}
}

// TestWithLocation 验证 WithLocation。
func TestWithLocation(t *testing.T) {
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	gen := NewGenerator(
		WithLocation(tokyo),
		WithSuffixDigits(4),
	)

	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tokyoDate := time.Now().In(tokyo).Format("20060102")
	if !strings.HasPrefix(code, tokyoDate) {
		t.Errorf("expected Tokyo date %s, got %s", tokyoDate, code)
	}
}

// TestDefaultGenerator 验证 DefaultGenerator。
func TestDefaultGenerator(t *testing.T) {
	gen := NewGenerator()
	code, err := gen.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	matched, _ := regexp.MatchString(`^\d{14}$`, code)
	if !matched {
		t.Errorf("expected 14 digits, got %s", code)
	}
}
