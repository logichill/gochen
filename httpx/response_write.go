package httpx

import (
	"fmt"
	"net/http"

	"gochen/errors"
)

// newSuccessMessageWithMessage 创建带自定义 message 的成功响应。
func newSuccessMessageWithMessage(data any, message string) *ResponseMessage {
	payload := NewSuccessMessage(data)
	if message != "" {
		payload.Message = message
	}
	return payload
}

// WriteSuccess 写入 200 成功响应。
func WriteSuccess(ctx IContext, data any) error {
	return WriteSuccessStatus(ctx, http.StatusOK, data)
}

// WriteCreated 写入 201 成功响应。
func WriteCreated(ctx IContext, data any) error {
	return WriteSuccessStatus(ctx, http.StatusCreated, data)
}

// WriteAccepted 写入 202 成功响应。
func WriteAccepted(ctx IContext, data any) error {
	return WriteSuccessStatus(ctx, http.StatusAccepted, data)
}

// WriteSuccessStatus 按指定 2xx 状态写入统一成功响应。
func WriteSuccessStatus(ctx IContext, status int, data any) error {
	return WriteSuccessMessage(ctx, status, "success", data)
}

// WriteSuccessMessage 按指定状态与 message 写入统一成功响应。
func WriteSuccessMessage(ctx IContext, status int, message string, data any) error {
	if ctx == nil {
		return fmt.Errorf("httpx: ctx is nil")
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("httpx: success status must be 2xx, got %d", status)
	}
	if !statusAllowsBody(status) {
		return fmt.Errorf("httpx: success status must allow body, got %d", status)
	}
	return ctx.JSON(status, JSONValue(newSuccessMessageWithMessage(data, message)))
}

// WriteNoContent 写入 204 空响应。
func WriteNoContent(ctx IContext) error {
	if ctx == nil {
		return fmt.Errorf("httpx: ctx is nil")
	}
	return ctx.String(http.StatusNoContent, "")
}

// WriteRedirectJSON 写入RedirectJSON。
func WriteRedirectJSON(ctx IContext, status int, location string, data any) error {
	if ctx == nil {
		return fmt.Errorf("httpx: ctx is nil")
	}
	if status < 300 || status >= 400 {
		return fmt.Errorf("httpx: redirect status must be 3xx, got %d", status)
	}
	if !statusAllowsBody(status) {
		return fmt.Errorf("httpx: redirect status must allow body, got %d", status)
	}
	ctx.SetHeader("Location", location)
	return ctx.JSON(status, JSONValue(&ResponseMessage{
		Code:    "redirect",
		Message: "redirect",
		Data:    data,
	}))
}

// WriteError 写入统一错误响应。
func WriteError(ctx IContext, err error) error {
	if ctx == nil {
		return fmt.Errorf("httpx: ctx is nil")
	}
	if err == nil {
		return nil
	}
	status, payload := EncodeErrorResponse(ctx, err)
	if payload == nil {
		return nil
	}
	return ctx.JSON(status, JSONValue(payload))
}

// WriteErrorExtra 写入统一错误响应，并携带结构化扩展字段。
func WriteErrorExtra(ctx IContext, err error, extra map[string]any) error {
	if ctx == nil {
		return fmt.Errorf("httpx: ctx is nil")
	}
	if err == nil {
		return nil
	}
	status, payload := EncodeErrorResponse(ctx, err)
	if payload == nil {
		return nil
	}
	if status < http.StatusInternalServerError && len(extra) > 0 {
		payload.Extra = extra
	}
	return ctx.JSON(status, JSONValue(payload))
}

// WriteErrorCode 按错误码和安全消息写入统一错误响应。
func WriteErrorCode(ctx IContext, code errors.ErrorCode, message string) error {
	return WriteError(ctx, errors.NewCode(code, message))
}

// WriteErrorCodeExtra 按错误码/消息与附加字段写入统一错误响应。
func WriteErrorCodeExtra(ctx IContext, code errors.ErrorCode, message string, extra map[string]any) error {
	return WriteErrorExtra(ctx, errors.NewCode(code, message), extra)
}

func statusAllowsBody(status int) bool {
	return !(status >= 100 && status < 200 || status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified)
}
