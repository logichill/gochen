package eventing

import "fmt"

// EventStoreError 事件存储错误基类
type EventStoreError struct {
	Code      string
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

// 说明：
//   - ConcurrencyError 本身就是业务错误的最终形态，不再包裹下层错误，因此不实现 Unwrap；
//   - 调用方应通过 errors.As(err, *ConcurrencyError) 或类型断言来识别并发冲突，而不是依赖 Unwrap 链。

func NewConcurrencyError(aggregateID int64, expected, actual uint64) *ConcurrencyError {
	return &ConcurrencyError{AggregateID: aggregateID, ExpectedVersion: expected, ActualVersion: actual}
}

var (
	ErrAggregateNotFound = &EventStoreError{Code: "AGGREGATE_NOT_FOUND", Message: "aggregate not found"}
	ErrInvalidEvent      = &EventStoreError{Code: "INVALID_EVENT", Message: "invalid event"}
	ErrEventNotFound     = &EventStoreError{Code: "EVENT_NOT_FOUND", Message: "event not found"}
	ErrStoreFailed       = &EventStoreError{Code: "STORE_FAILED", Message: "event store operation failed"}
)

type EventAlreadyExistsError struct {
	EventID     string
	AggregateID int64
	Version     uint64
}

func (e *EventAlreadyExistsError) Error() string {
	return fmt.Sprintf("event %s already exists for aggregate %d version %d", e.EventID, e.AggregateID, e.Version)
}

func NewEventAlreadyExistsError(eventID string, aggregateID int64, version uint64) *EventAlreadyExistsError {
	return &EventAlreadyExistsError{EventID: eventID, AggregateID: aggregateID, Version: version}
}
