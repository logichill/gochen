package httpx

import (
	"gochen/contextx"
	"gochen/errors"
	"net/http"
	"strings"
)

// EncodeErrorResponse 将 err 编码为统一的 HTTP 错误响应（status + JSON payload）。
//
// 说明：
// - 会先对 err 做 Normalize，确保尽可能使用统一的 ErrorCode 体系；
// - 5xx 场景会对 message 做安全兜底，避免泄露内部错误细节；
// - 若 ctx 携带 trace_id/request_id，则默认回传到错误 body，便于排障闭环。
func EncodeErrorResponse(ctx IContext, err error) (status int, payload *ResponseMessage) {
	if err == nil {
		return http.StatusOK, nil
	}

	normalized := errors.Normalize(err)
	status = errors.ToHTTPStatus(normalized)

	code := string(errors.Code(normalized))
	message := safeErrorMessage(status, normalized)

	payload = NewResponseMessage(code, message)
	if ctx != nil {
		if reqCtx := ctx.RequestContext(); reqCtx != nil {
			if v := contextx.TraceID(reqCtx); v != "" {
				payload.TraceID = v
			}
			if v := contextx.RequestID(reqCtx); v != "" {
				payload.RequestID = v
			}
		}
	}
	return status, payload
}

func safeErrorMessage(status int, err error) string {
	if err == nil {
		return ""
	}

	var message string
	var appErr *errors.AppError
	if errors.As(err, &appErr) && appErr != nil {
		message = appErr.Message()
	} else {
		message = err.Error()
	}

	if status >= http.StatusInternalServerError {
		if safe := strings.ToLower(http.StatusText(status)); safe != "" {
			return safe
		}
		return "internal server error"
	}
	return message
}
