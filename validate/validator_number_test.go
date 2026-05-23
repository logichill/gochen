package validate

import (
	"testing"
)

// TestIntRange 验证 IntRange。
func TestIntRange(t *testing.T) {
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
			err := IntRange(tt.value, tt.fieldName, tt.min, tt.max)
			if (err != nil) != tt.wantErr {
				t.Errorf("IntRange() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestPositive 验证 Positive。
func TestPositive(t *testing.T) {
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
			err := Positive(tt.value, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Positive() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestPageParams 验证 PageParams。
func TestPageParams(t *testing.T) {
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
			err := PageParams(tt.page, tt.pageSize)
			if (err != nil) != tt.wantErr {
				t.Errorf("PageParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestID 验证 ID。
func TestID(t *testing.T) {
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
			id:        9223372036854775807,
			fieldName: "用户ID",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ID(tt.id, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
