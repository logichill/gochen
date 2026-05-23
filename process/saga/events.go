package saga

// SagaEventType 定义Saga事件类型枚举。
type SagaEventType string

const (
	// Saga lifecycle event types
	EventSagaStarted SagaEventType = "SagaStarted"
	// EventSagaResumed 是常量。
	EventSagaResumed SagaEventType = "SagaResumed"
	// EventSagaStepCompleted 是常量。
	EventSagaStepCompleted SagaEventType = "SagaStepCompleted"
	// EventSagaStepFailed 是常量。
	EventSagaStepFailed SagaEventType = "SagaStepFailed"
	// EventSagaCompensationStarted 是常量。
	EventSagaCompensationStarted SagaEventType = "SagaCompensationStarted"
	// EventSagaCompensationStepCompleted 是常量。
	EventSagaCompensationStepCompleted SagaEventType = "SagaCompensationStepCompleted"
	// EventSagaCompensationStepFailed 是常量。
	EventSagaCompensationStepFailed SagaEventType = "SagaCompensationStepFailed"
	// EventSagaCompensationCompleted 是常量。
	EventSagaCompensationCompleted SagaEventType = "SagaCompensationCompleted"
	// EventSagaCompleted 是常量。
	EventSagaCompleted SagaEventType = "SagaCompleted"
	// EventSagaFailed 是常量。
	EventSagaFailed SagaEventType = "SagaFailed"
)

func (t SagaEventType) String() string { return string(t) }
