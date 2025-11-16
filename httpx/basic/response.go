package basic

import (
	httpx "gochen/httpx"
	"net/http"
)

type JSONResponse struct {
	status int
	body   interface{}
}

func (r *JSONResponse) Send(ctx httpx.IHttpContext) error { return ctx.JSON(r.status, r.body) }

func SuccessResponse(data interface{}) httpx.IResponse {
	payload := map[string]interface{}{"code": 0, "message": "success", "data": data}
	return &JSONResponse{status: http.StatusOK, body: payload}
}

func ErrorResponse(status int, message string) httpx.IResponse {
	payload := map[string]interface{}{"code": status, "message": message}
	return &JSONResponse{status: status, body: payload}
}
