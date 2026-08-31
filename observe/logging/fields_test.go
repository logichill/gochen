package logging

import (
	"gochen/errors"
	"testing"
)

// TestFieldConstructors 验证 FieldConstructors。
func TestFieldConstructors(t *testing.T) {
	tests := []struct {
		name     string
		field    Field
		wantKey  string
		wantType string
	}{
		{
			name:     "String字段",
			field:    String("name", "test"),
			wantKey:  "name",
			wantType: "string",
		},
		{
			name:     "Int字段",
			field:    Int("count", 123),
			wantKey:  "count",
			wantType: "int",
		},
		{
			name:     "Int64字段",
			field:    Int64("id", int64(456)),
			wantKey:  "id",
			wantType: "int64",
		},
		{
			name:     "Uint64字段",
			field:    Uint64("timestamp", uint64(789)),
			wantKey:  "timestamp",
			wantType: "uint64",
		},
		{
			name:     "Float64字段",
			field:    Float64("price", 12.34),
			wantKey:  "price",
			wantType: "float64",
		},
		{
			name:     "Bool字段",
			field:    Bool("active", true),
			wantKey:  "active",
			wantType: "bool",
		},
		{
			name:     "Any字段",
			field:    Any("data", map[string]int{"a": 1}),
			wantKey:  "data",
			wantType: "any",
		},
		{
			name:     "Error字段",
			field:    Error(errors.New("test error")),
			wantKey:  "error",
			wantType: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.field.Key != tt.wantKey {
				t.Errorf("Key = %s, 期望 %s", tt.field.Key, tt.wantKey)
			}
			if tt.field.Value == nil {
				t.Error("Value为nil")
			}
		})
	}
}

// TestFormatValue 验证 FormatValue。
func TestFormatValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "字符串",
			value: "test",
			want:  "test",
		},
		{
			name:  "错误",
			value: errors.New("error message"),
			want:  "error message",
		},
		{
			name:  "整数",
			value: 123,
			want:  "123",
		},
		{
			name:  "布尔值",
			value: true,
			want:  "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatValue(tt.value)
			if got != tt.want {
				t.Errorf("formatValue() = %s, 期望 %s", got, tt.want)
			}
		})
	}
}

// BenchmarkFieldConstructors 用于评估 FieldConstructors 的性能。
func BenchmarkFieldConstructors(b *testing.B) {
	b.Run("String", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			String("key", "value")
		}
	})

	b.Run("Int", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Int("count", 123)
		}
	})

	b.Run("Error", func(b *testing.B) {
		err := errors.New("test error")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Error(err)
		}
	})
}
