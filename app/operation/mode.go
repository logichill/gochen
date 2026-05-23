package operation

// Mode 表示写操作的执行模式。
type Mode string

const (
	// ModeInline 表示请求返回时已完成收敛。
	ModeInline Mode = "inline"
	// ModeTracked 表示请求已接受，但仍需后续跟踪。
	ModeTracked Mode = "tracked"
)

// IsValid 判断 mode 是否为已知值。
func (m Mode) IsValid() bool {
	switch m {
	case ModeInline, ModeTracked:
		return true
	default:
		return false
	}
}
