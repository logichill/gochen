package operation

import (
	"gochen/internal/clonevalue"
)

// CloneResult 深拷贝 operation result，避免调用方共享内部可变引用。
func CloneResult(result *Result) *Result {
	if result == nil {
		return nil
	}

	cloned := *result
	if result.Resource != nil {
		resource := *result.Resource
		cloned.Resource = &resource
	}
	if result.Error != nil {
		errCopy := *result.Error
		if result.Error.Details != nil {
			errCopy.Details = clonevalue.MapStringAny(result.Error.Details)
		}
		cloned.Error = &errCopy
	}
	if result.Result != nil {
		cloned.Result = clonevalue.MapStringAny(result.Result)
	}
	if result.AffectedScopes != nil {
		cloned.AffectedScopes = append([]string(nil), result.AffectedScopes...)
	}
	return &cloned
}
