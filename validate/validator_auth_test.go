package validate

import (
	"strings"
	"testing"
)

// TestUsername 验证 Username。
func TestUsername(t *testing.T) {
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
			err := Username(tt.username)
			if (err != nil) != tt.wantErr {
				t.Errorf("Username() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestPassword 验证 Password。
func TestPassword(t *testing.T) {
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
			err := Password(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("Password() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
