// Package entity 定义聚合根相关错误
package entity

// 常见聚合根错误
var (
	ErrAggregateDeleted   = &EntityError{Code: "AGGREGATE_DELETED", Message: "aggregate is deleted"}
	ErrAggregateNotFound  = &EntityError{Code: "AGGREGATE_NOT_FOUND", Message: "aggregate not found"}
	ErrInvalidAggregateID = &EntityError{Code: "INVALID_AGGREGATE_ID", Message: "invalid aggregate id"}
	ErrEventApplyFailed   = &EntityError{Code: "EVENT_APPLY_FAILED", Message: "failed to apply event"}
)

// AggregateError 聚合根错误
type AggregateError struct {
	Code        string
	Message     string
	AggregateID any
	EventID     string
	Cause       error
}

func (e *AggregateError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *AggregateError) Unwrap() error {
	return e.Cause
}
