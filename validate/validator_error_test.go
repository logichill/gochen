package validate

import (
	"testing"

	"gochen/errors"
)

// TestValidationErrorCode 验证 ValidationErrorCode。
func TestValidationErrorCode(t *testing.T) {
	err := Required("", "字段")
	if err == nil {
		t.Fatal("期望返回错误")
	}

	if !errors.Is(err, errors.Validation) {
		t.Error("错误码不是VALIDATION_ERROR")
	}
}
