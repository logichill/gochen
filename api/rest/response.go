package rest

import (
	core "gochen/httpx"
)

type jsonResponse struct {
	status int
	body   any
}

func (r *jsonResponse) Send(ctx core.IContext) error {
	return ctx.JSON(r.status, core.JSONValue(r.body))
}

func successResponse(data any) core.IResponse {
	return &jsonResponse{
		status: 200,
		body:   core.NewSuccessMessage(data),
	}
}
