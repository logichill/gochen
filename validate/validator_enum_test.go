package validate

import (
	"testing"
)

// TestEnum 验证 Enum。
func TestEnum(t *testing.T) {
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
			err := Enum(tt.value, tt.fieldName, validValues)
			if (err != nil) != tt.wantErr {
				t.Errorf("Enum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
