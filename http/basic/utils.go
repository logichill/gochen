package basic

import (
	"fmt"
	"net/http"
	"strconv"

	"gochen/errors"
	httpx "gochen/http"
)

type HttpUtils struct{}

func (u *HttpUtils) ParseID(ctx httpx.IHttpContext, paramName string) (int64, error) {
	idStr := ctx.GetParam(paramName)
	if idStr == "" {
		return 0, errors.NewError(errors.ErrCodeInvalidInput, fmt.Sprintf("parameter %s cannot be empty", paramName))
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, errors.WrapError(err, errors.ErrCodeInvalidInput, fmt.Sprintf("parameter %s must be a valid integer", paramName))
	}
	if id <= 0 {
		return 0, errors.NewError(errors.ErrCodeInvalidInput, fmt.Sprintf("parameter %s must be greater than 0", paramName))
	}
	return id, nil
}

func (u *HttpUtils) ParsePagination(ctx httpx.IHttpContext) (*httpx.ListRequest, error) {
	pageStr := ctx.GetQuery("page")
	if pageStr == "" {
		pageStr = "1"
	}
	pageSizeStr := ctx.GetQuery("page_size")
	if pageSizeStr == "" {
		pageSizeStr = "20"
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil || page <= 0 {
		return nil, errors.NewError(errors.ErrCodeInvalidInput, "page number must be a positive integer")
	}
	pageSize, err := strconv.Atoi(pageSizeStr)
	if err != nil || pageSize <= 0 || pageSize > 1000 {
		return nil, errors.NewError(errors.ErrCodeInvalidInput, "page size must be between 1 and 1000")
	}
	req := httpx.NewListRequest(page, pageSize)
	if sortBy := ctx.GetQuery("sort_by"); sortBy != "" {
		sortDir := ctx.GetQuery("sort_dir")
		if sortDir == "" {
			sortDir = "asc"
		} else if sortDir != "asc" && sortDir != "desc" {
			return nil, errors.NewError(errors.ErrCodeInvalidInput, "sort_dir must be 'asc' or 'desc'")
		}
		req.SetSort(sortBy, sortDir)
	}
	return req, nil
}

func (u *HttpUtils) WriteErrorResponse(ctx httpx.IHttpContext, err error) error {
	if v, ok := ctx.Get("response_written"); ok {
		if written, _ := v.(bool); written {
			return nil
		}
	}

	// 优先规范化错误，确保尽可能使用统一的 ErrorCode 体系
	err = errors.Normalize(err)

	var (
		status    int
		message   string
		errorCode string
	)
	if appErr, ok := err.(errors.IError); ok {
		// 映射错误码到 HTTP 状态码
		switch appErr.Code() {
		case errors.ErrCodeNotFound:
			status = http.StatusNotFound
		case errors.ErrCodeInvalidInput, errors.ErrCodeValidation:
			status = http.StatusBadRequest
		case errors.ErrCodeUnauthorized:
			status = http.StatusUnauthorized
		case errors.ErrCodeForbidden:
			status = http.StatusForbidden
		case errors.ErrCodeConflict:
			status = http.StatusConflict
		case errors.ErrCodeTimeout:
			status = http.StatusRequestTimeout
		case errors.ErrCodeTooManyRequests:
			status = http.StatusTooManyRequests
		case errors.ErrCodeServiceUnavailable:
			status = http.StatusServiceUnavailable
		default:
			status = http.StatusInternalServerError
		}
		message = appErr.Message()
		errorCode = string(appErr.Code())
	} else {
		status = http.StatusInternalServerError
		message = err.Error()
		errorCode = string(errors.ErrCodeInternal)
	}
	if jerr := ctx.JSON(status, httpx.NewErrorResponse(errorCode, message, "")); jerr != nil {
		_ = ctx.String(http.StatusInternalServerError, fmt.Sprintf("%s: %s", errorCode, message))
	}
	// 标记已写出错误响应，避免在同一请求链路中重复写入
	ctx.Set("response_written", true)
	return nil
}

func (u *HttpUtils) WriteSuccessResponse(ctx httpx.IHttpContext, data any) error {
	if jerr := ctx.JSON(http.StatusOK, httpx.NewSuccessResponse(data)); jerr != nil {
		_ = ctx.String(http.StatusOK, "success")
	}
	return nil
}
