package errors

import (
	"fmt"
	"runtime"
	"strings"
)

// shouldCaptureStack 判断该错误码是否应自动附带堆栈信息。
func shouldCaptureStack(code ErrorCode) bool {
	return code.IsSystemFault() || ErrorCodeToHTTPStatus(code) >= 500
}

// stackFromCause 尝试从底层错误链中提取已有堆栈。
func stackFromCause(cause error) (string, bool) {
	if cause == nil {
		return "", false
	}
	var appErr *AppError
	if !As(cause, &appErr) || appErr == nil {
		return "", false
	}
	stack, ok := appErr.details["stack"].(string)
	if !ok || strings.TrimSpace(stack) == "" {
		return "", false
	}
	return stack, true
}

func captureStack(skip int) string {
	const maxDepth = 32
	pcs := make([]uintptr, maxDepth)
	n := runtime.Callers(skip, pcs)
	pcs = pcs[:n]

	frames := runtime.CallersFrames(pcs)
	var sb strings.Builder
	for {
		frame, more := frames.Next()
		if frame.Function != "" {
			sb.WriteString(frame.Function)
		} else {
			sb.WriteString("<unknown>")
		}
		sb.WriteByte('\n')
		fmt.Fprintf(&sb, "\t%s:%d\n", frame.File, frame.Line)
		if !more {
			break
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
