package operation

import (
	"strings"

	"gochen/errors"
)

func defaultStatusForMode(mode Mode) Status {
	if mode == ModeTracked {
		return StatusAccepted
	}
	return StatusSettled
}

func successStatusForResult(result *Result, mode Mode) Status {
	if mode == ModeInline {
		return StatusSettled
	}
	if result != nil && result.Operation.Status.IsValid() && result.Operation.Status != StatusFailed {
		return result.Operation.Status
	}
	return defaultStatusForMode(mode)
}

func operationErrorFromError(err error) *OperationError {
	if err == nil {
		return nil
	}
	code := errors.Code(err)
	if code == "" {
		code = errors.Internal
	}
	message := publicOperationErrorMessage(code)
	var appErr *errors.AppError
	var details map[string]any
	if errors.As(err, &appErr) && appErr != nil {
		message = appErr.Message()
		details = sanitizeOperationErrorDetails(appErr.Details())
	}
	return &OperationError{
		Code:    string(code),
		Message: message,
		Details: details,
	}
}

func publicOperationErrorMessage(code errors.ErrorCode) string {
	if message := errors.PublicMessage(code); message != "" {
		return message
	}
	return "operation failed"
}

func sanitizeOperationErrorDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]any, len(details))
	for key, value := range details {
		if isSensitiveOperationErrorDetail(key) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isSensitiveOperationErrorDetail(key string) bool {
	switch strings.TrimSpace(strings.ToLower(key)) {
	case "stack", "panic", "panic_stack", "cause", "error", "err", "raw_error":
		return true
	default:
		return false
	}
}

func mergeScopes(groups ...[]string) []string {
	if len(groups) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, group := range groups {
		for _, scope := range group {
			if scope == "" {
				continue
			}
			if _, ok := seen[scope]; ok {
				continue
			}
			seen[scope] = struct{}{}
			merged = append(merged, scope)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
