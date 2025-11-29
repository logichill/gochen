package saga

const (
	// Saga lifecycle event types
	EventSagaStarted                = "SagaStarted"
	EventSagaStepCompleted          = "SagaStepCompleted"
	EventSagaStepFailed             = "SagaStepFailed"
	EventSagaCompensationStarted    = "SagaCompensationStarted"
	EventSagaCompensationStepFailed = "SagaCompensationStepFailed"
	EventSagaCompensationCompleted  = "SagaCompensated"
	EventSagaCompleted              = "SagaCompleted"
	EventSagaFailed                 = "SagaFailed"
)
