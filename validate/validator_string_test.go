package validate

import (
	"testing"
)

// TestStringLength 验证 StringLength。
func TestStringLength(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fieldName string
		min       int
		max       int
		wantErr   bool
	}{
		{
			name:      "有效长度",
			value:     "hello",
			fieldName: "字段",
			min:       3,
			max:       10,
			wantErr:   false,
		},
		{
			name:      "长度太短",
			value:     "ab",
			fieldName: "字段",
			min:       3,
			max:       10,
			wantErr:   true,
		},
		{
			name:      "长度太长",
			value:     "abcdefghijk",
			fieldName: "字段",
			min:       3,
			max:       10,
			wantErr:   true,
		},
		{
			name:      "最小边界值",
			value:     "abc",
			fieldName: "字段",
			min:       3,
			max:       10,
			wantErr:   false,
		},
		{
			name:      "最大边界值",
			value:     "abcdefghij",
			fieldName: "字段",
			min:       3,
			max:       10,
			wantErr:   false,
		},
		{
			name:      "无最大限制",
			value:     "very long string that exceeds normal limits",
			fieldName: "字段",
			min:       3,
			max:       0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := StringLength(tt.value, tt.fieldName, tt.min, tt.max)
			if (err != nil) != tt.wantErr {
				t.Errorf("StringLength() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRequired 验证 Required。
func TestRequired(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fieldName string
		wantErr   bool
	}{
		{
			name:      "有效值",
			value:     "hello",
			fieldName: "字段",
			wantErr:   false,
		},
		{
			name:      "空字符串",
			value:     "",
			fieldName: "字段",
			wantErr:   true,
		},
		{
			name:      "空格字符串",
			value:     "   ",
			fieldName: "字段",
			wantErr:   true,
		},
		{
			name:      "带前后空格的有效值",
			value:     "  hello  ",
			fieldName: "字段",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Required(tt.value, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Required() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestEmail 验证 Email。
func TestEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{
			name:    "有效邮箱",
			email:   "test@example.com",
			wantErr: false,
		},
		{
			name:    "带加号的邮箱",
			email:   "test+tag@example.com",
			wantErr: false,
		},
		{
			name:    "带下划线的邮箱",
			email:   "test_user@example.com",
			wantErr: false,
		},
		{
			name:    "空邮箱",
			email:   "",
			wantErr: true,
		},
		{
			name:    "无@符号",
			email:   "testexample.com",
			wantErr: true,
		},
		{
			name:    "无域名",
			email:   "test@",
			wantErr: true,
		},
		{
			name:    "无用户名",
			email:   "@example.com",
			wantErr: true,
		},
		{
			name:    "无顶级域",
			email:   "test@example",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Email(tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("Email() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
