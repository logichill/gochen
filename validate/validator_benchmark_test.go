package validate

import "testing"

// BenchmarkStringLength 用于评估 StringLength 的性能。
func BenchmarkStringLength(b *testing.B) {
	value := "test string"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StringLength(value, "字段", 3, 50)
	}
}

// BenchmarkEmail 用于评估 Email 的性能。
func BenchmarkEmail(b *testing.B) {
	email := "test@example.com"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Email(email)
	}
}

// BenchmarkUsername 用于评估 Username 的性能。
func BenchmarkUsername(b *testing.B) {
	username := "testuser123"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Username(username)
	}
}

// BenchmarkEnum 用于评估 Enum 的性能。
func BenchmarkEnum(b *testing.B) {
	validValues := []string{"active", "inactive", "pending"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Enum("active", "状态", validValues)
	}
}
