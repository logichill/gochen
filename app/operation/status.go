package operation

// Status 表示 operation 的生命周期状态。
type Status string

const (
	StatusAccepted   Status = "accepted"
	StatusProcessing Status = "processing"
	StatusSettled    Status = "settled"
	StatusFailed     Status = "failed"
	StatusTimeout    Status = "timeout"
	StatusDegraded   Status = "degraded"
)

// IsValid 判断 status 是否为已知值。
func (s Status) IsValid() bool {
	switch s {
	case StatusAccepted, StatusProcessing, StatusSettled, StatusFailed, StatusTimeout, StatusDegraded:
		return true
	default:
		return false
	}
}
