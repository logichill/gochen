package http

import "time"

const (
	OK       int = 200
	Created  int = 201
	BadReq   int = 400
	Unauth   int = 401
	Forbid   int = 403
	NotFound int = 404
	Error    int = 500
)

type Message struct {
	Code int       `json:"code"`
	Msg  string    `json:"msg"`
	Data any       `json:"data"`
	Time time.Time `json:"time"`
}

func SuccessMessage(data any) *Message {
	return &Message{
		Code: OK,
		Data: data,
		Time: time.Now(),
	}
}

func ErrorMessage(code int, msg string) *Message {
	return &Message{
		Code: code,
		Msg:  msg,
		Time: time.Now(),
	}
}
