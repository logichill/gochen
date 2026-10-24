package outbox

type outboxFailureStage uint8

const (
	outboxFailureDeserialize outboxFailureStage = iota
	outboxFailurePublish
)

func (s outboxFailureStage) warnLogMessage() string {
	switch s {
	case outboxFailureDeserialize:
		return "outbox deserialize failed"
	case outboxFailurePublish:
		return "outbox publish failed"
	default:
		return "outbox entry failed"
	}
}
