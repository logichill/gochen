package eventing

import "fmt"

// ErrorCode 事件存储错误码
type ErrorCode string

// 预定义错误码常量（不可变）
const (
	ErrCodeAggregateNotFound   ErrorCode = "AGGREGATE_NOT_FOUND"
	ErrCodeInvalidEvent        ErrorCode = "INVALID_EVENT"
	ErrCodeEventNotFound       ErrorCode = "EVENT_NOT_FOUND"
	ErrCodeStoreFailed         ErrorCode = "STORE_FAILED"
	ErrCodeEventExists         ErrorCode = "EVENT_ALREADY_EXISTS"
	ErrCodeConcurrency         ErrorCode = "CONCURRENCY_CONFLICT"
	ErrCodeSerializePayload    ErrorCode = "SERIALIZE_PAYLOAD_FAILED"
	ErrCodeSerializeMetadata   ErrorCode = "SERIALIZE_METADATA_FAILED"
)

// EventStoreError 事件存储错误
type EventStoreError struct {
	Code      ErrorCode
	Message   string
	Cause     error
	EventID   string
	EventType string
}

func (e *EventStoreError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *EventStoreError) Unwrap() error { return e.Cause }

// Is 实现 errors.Is 接口，基于错误码匹配
func (e *EventStoreError) Is(target error) bool {
	t, ok := target.(*EventStoreError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// 哨兵错误（仅用于 errors.Is 比较，不应直接返回）
var (
	errAggregateNotFound = &EventStoreError{Code: ErrCodeAggregateNotFound}
	errInvalidEvent      = &EventStoreError{Code: ErrCodeInvalidEvent}
	errEventNotFound     = &EventStoreError{Code: ErrCodeEventNotFound}
	errStoreFailed       = &EventStoreError{Code: ErrCodeStoreFailed}
)

// ErrAggregateNotFound 返回聚合未找到错误（用于 errors.Is 比较）
func ErrAggregateNotFound() *EventStoreError {
	return errAggregateNotFound
}

// ErrInvalidEvent 返回无效事件错误（用于 errors.Is 比较）
func ErrInvalidEvent() *EventStoreError {
	return errInvalidEvent
}

// ErrEventNotFound 返回事件未找到错误（用于 errors.Is 比较）
func ErrEventNotFound() *EventStoreError {
	return errEventNotFound
}

// ErrStoreFailed 返回存储失败错误（用于 errors.Is 比较）
func ErrStoreFailed() *EventStoreError {
	return errStoreFailed
}

// NewAggregateNotFoundError 创建聚合未找到错误
func NewAggregateNotFoundError(aggregateID int64) *EventStoreError {
	return &EventStoreError{
		Code:    ErrCodeAggregateNotFound,
		Message: fmt.Sprintf("aggregate %d not found", aggregateID),
	}
}

// NewInvalidEventError 创建无效事件错误
func NewInvalidEventError(eventID, eventType, reason string) *EventStoreError {
	return &EventStoreError{
		Code:      ErrCodeInvalidEvent,
		Message:   reason,
		EventID:   eventID,
		EventType: eventType,
	}
}

// NewInvalidEventErrorWithCause 创建带原因的无效事件错误
func NewInvalidEventErrorWithCause(eventID, eventType, reason string, cause error) *EventStoreError {
	return &EventStoreError{
		Code:      ErrCodeInvalidEvent,
		Message:   reason,
		Cause:     cause,
		EventID:   eventID,
		EventType: eventType,
	}
}

// NewEventNotFoundError 创建事件未找到错误
func NewEventNotFoundError(eventID string) *EventStoreError {
	return &EventStoreError{
		Code:    ErrCodeEventNotFound,
		Message: fmt.Sprintf("event %s not found", eventID),
		EventID: eventID,
	}
}

// NewStoreFailedError 创建存储失败错误
func NewStoreFailedError(message string, cause error) *EventStoreError {
	return &EventStoreError{
		Code:    ErrCodeStoreFailed,
		Message: message,
		Cause:   cause,
	}
}

// NewStoreFailedErrorWithEvent 创建带事件信息的存储失败错误
func NewStoreFailedErrorWithEvent(message string, cause error, eventID, eventType string) *EventStoreError {
	return &EventStoreError{
		Code:      ErrCodeStoreFailed,
		Message:   message,
		Cause:     cause,
		EventID:   eventID,
		EventType: eventType,
	}
}

// ConcurrencyError 并发冲突错误
type ConcurrencyError struct {
	AggregateID     int64
	ExpectedVersion uint64
	ActualVersion   uint64
}

func (e *ConcurrencyError) Error() string {
	return fmt.Sprintf("concurrency conflict: aggregate %d expected version %d, actual version %d",
		e.AggregateID, e.ExpectedVersion, e.ActualVersion)
}

// Is 实现 errors.Is 接口
func (e *ConcurrencyError) Is(target error) bool {
	_, ok := target.(*ConcurrencyError)
	return ok
}

func NewConcurrencyError(aggregateID int64, expected, actual uint64) *ConcurrencyError {
	return &ConcurrencyError{AggregateID: aggregateID, ExpectedVersion: expected, ActualVersion: actual}
}

// EventAlreadyExistsError 事件已存在错误
type EventAlreadyExistsError struct {
	EventID     string
	AggregateID int64
	Version     uint64
}

func (e *EventAlreadyExistsError) Error() string {
	return fmt.Sprintf("event %s already exists for aggregate %d version %d", e.EventID, e.AggregateID, e.Version)
}

// Is 实现 errors.Is 接口
func (e *EventAlreadyExistsError) Is(target error) bool {
	_, ok := target.(*EventAlreadyExistsError)
	return ok
}

func NewEventAlreadyExistsError(eventID string, aggregateID int64, version uint64) *EventAlreadyExistsError {
	return &EventAlreadyExistsError{EventID: eventID, AggregateID: aggregateID, Version: version}
}
