package validation

import (
	"strings"
	"testing"

	sharederrors "gochen/errors"
)

// TestValidateStringLength 测试字符串长度验证
func TestValidateStringLength(t *testing.T) {
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
			max:       0, // 0表示无限制
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStringLength(tt.value, tt.fieldName, tt.min, tt.max)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStringLength() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateRequired 测试必填验证
func TestValidateRequired(t *testing.T) {
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
			err := ValidateRequired(tt.value, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRequired() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateIntRange 测试整数范围验证
func TestValidateIntRange(t *testing.T) {
	tests := []struct {
		name      string
		value     int
		fieldName string
		min       int
		max       int
		wantErr   bool
	}{
		{
			name:      "有效范围",
			value:     50,
			fieldName: "数量",
			min:       1,
			max:       100,
			wantErr:   false,
		},
		{
			name:      "小于最小值",
			value:     0,
			fieldName: "数量",
			min:       1,
			max:       100,
			wantErr:   true,
		},
		{
			name:      "大于最大值",
			value:     101,
			fieldName: "数量",
			min:       1,
			max:       100,
			wantErr:   true,
		},
		{
			name:      "最小边界",
			value:     1,
			fieldName: "数量",
			min:       1,
			max:       100,
			wantErr:   false,
		},
		{
			name:      "最大边界",
			value:     100,
			fieldName: "数量",
			min:       1,
			max:       100,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIntRange(tt.value, tt.fieldName, tt.min, tt.max)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIntRange() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidatePositive 测试正数验证
func TestValidatePositive(t *testing.T) {
	tests := []struct {
		name      string
		value     int
		fieldName string
		wantErr   bool
	}{
		{
			name:      "正数",
			value:     10,
			fieldName: "数量",
			wantErr:   false,
		},
		{
			name:      "零",
			value:     0,
			fieldName: "数量",
			wantErr:   true,
		},
		{
			name:      "负数",
			value:     -5,
			fieldName: "数量",
			wantErr:   true,
		},
		{
			name:      "最小正数",
			value:     1,
			fieldName: "数量",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePositive(tt.value, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePositive() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateEmail 测试邮箱验证
func TestValidateEmail(t *testing.T) {
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
			err := ValidateEmail(tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEmail() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateUsername 测试用户名验证
func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{
			name:     "有效用户名",
			username: "testuser",
			wantErr:  false,
		},
		{
			name:     "带数字的用户名",
			username: "test123",
			wantErr:  false,
		},
		{
			name:     "带下划线的用户名",
			username: "test_user",
			wantErr:  false,
		},
		{
			name:     "空用户名",
			username: "",
			wantErr:  true,
		},
		{
			name:     "太短的用户名",
			username: "ab",
			wantErr:  true,
		},
		{
			name:     "太长的用户名",
			username: strings.Repeat("a", 51),
			wantErr:  true,
		},
		{
			name:     "包含特殊字符",
			username: "test@user",
			wantErr:  true,
		},
		{
			name:     "包含空格",
			username: "test user",
			wantErr:  true,
		},
		{
			name:     "最小长度边界",
			username: "abc",
			wantErr:  false,
		},
		{
			name:     "最大长度边界",
			username: strings.Repeat("a", 50),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUsername() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidatePassword 测试密码验证
func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "有效密码",
			password: "password123",
			wantErr:  false,
		},
		{
			name:     "空密码",
			password: "",
			wantErr:  true,
		},
		{
			name:     "太短的密码",
			password: "12345",
			wantErr:  true,
		},
		{
			name:     "太长的密码",
			password: strings.Repeat("a", 101),
			wantErr:  true,
		},
		{
			name:     "最小长度边界",
			password: "123456",
			wantErr:  false,
		},
		{
			name:     "最大长度边界",
			password: strings.Repeat("a", 100),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePassword() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateEnum 测试枚举验证
func TestValidateEnum(t *testing.T) {
	validValues := []string{"active", "inactive", "pending"}

	tests := []struct {
		name      string
		value     string
		fieldName string
		wantErr   bool
	}{
		{
			name:      "有效值-第一个",
			value:     "active",
			fieldName: "状态",
			wantErr:   false,
		},
		{
			name:      "有效值-中间",
			value:     "inactive",
			fieldName: "状态",
			wantErr:   false,
		},
		{
			name:      "有效值-最后",
			value:     "pending",
			fieldName: "状态",
			wantErr:   false,
		},
		{
			name:      "无效值",
			value:     "deleted",
			fieldName: "状态",
			wantErr:   true,
		},
		{
			name:      "空值",
			value:     "",
			fieldName: "状态",
			wantErr:   true,
		},
		{
			name:      "大小写不匹配",
			value:     "ACTIVE",
			fieldName: "状态",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEnum(tt.value, tt.fieldName, validValues)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEnum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidatePageParams 测试分页参数验证
func TestValidatePageParams(t *testing.T) {
	tests := []struct {
		name     string
		page     int
		pageSize int
		wantErr  bool
	}{
		{
			name:     "有效分页",
			page:     1,
			pageSize: 20,
			wantErr:  false,
		},
		{
			name:     "页码为0",
			page:     0,
			pageSize: 20,
			wantErr:  true,
		},
		{
			name:     "页码为负数",
			page:     -1,
			pageSize: 20,
			wantErr:  true,
		},
		{
			name:     "每页大小为0",
			page:     1,
			pageSize: 0,
			wantErr:  true,
		},
		{
			name:     "每页大小为负数",
			page:     1,
			pageSize: -10,
			wantErr:  true,
		},
		{
			name:     "每页大小超过100",
			page:     1,
			pageSize: 101,
			wantErr:  true,
		},
		{
			name:     "最大每页大小边界",
			page:     1,
			pageSize: 100,
			wantErr:  false,
		},
		{
			name:     "最小有效值",
			page:     1,
			pageSize: 1,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePageParams(tt.page, tt.pageSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePageParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateID 测试ID验证
func TestValidateID(t *testing.T) {
	tests := []struct {
		name      string
		id        int64
		fieldName string
		wantErr   bool
	}{
		{
			name:      "有效ID",
			id:        123,
			fieldName: "用户ID",
			wantErr:   false,
		},
		{
			name:      "零ID",
			id:        0,
			fieldName: "用户ID",
			wantErr:   true,
		},
		{
			name:      "负数ID",
			id:        -5,
			fieldName: "用户ID",
			wantErr:   true,
		},
		{
			name:      "最小有效ID",
			id:        1,
			fieldName: "用户ID",
			wantErr:   false,
		},
		{
			name:      "大数ID",
			id:        9223372036854775807, // int64最大值
			fieldName: "用户ID",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateID(tt.id, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidationErrorCode 测试验证错误返回正确的错误码
func TestValidationErrorCode(t *testing.T) {
	err := ValidateRequired("", "字段")
	if err == nil {
		t.Fatal("期望返回错误")
	}

	if !sharederrors.IsValidation(err) {
		t.Error("错误码不是VALIDATION_ERROR")
	}
}

// BenchmarkValidateStringLength 基准测试：字符串长度验证
func BenchmarkValidateStringLength(b *testing.B) {
	value := "test string"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateStringLength(value, "字段", 3, 50)
	}
}

// BenchmarkValidateEmail 基准测试：邮箱验证
func BenchmarkValidateEmail(b *testing.B) {
	email := "test@example.com"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateEmail(email)
	}
}

// BenchmarkValidateUsername 基准测试：用户名验证
func BenchmarkValidateUsername(b *testing.B) {
	username := "testuser123"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateUsername(username)
	}
}

// BenchmarkValidateEnum 基准测试：枚举验证
func BenchmarkValidateEnum(b *testing.B) {
	validValues := []string{"active", "inactive", "pending"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateEnum("active", "状态", validValues)
	}
}
