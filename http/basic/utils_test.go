package basic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gochen/errors"
	httpx "gochen/http"
)

// helper to build a basic IHttpContext for tests.
func newTestContext(method, target string) *HttpContext {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	return NewBaseHttpContext(rec, req)
}

func TestHttpUtils_ParseID(t *testing.T) {
	utils := &HttpUtils{}

	// 正常路径
	ctx := newTestContext(http.MethodGet, "/users/123")
	ctx.SetParam("id", "123")

	id, err := utils.ParseID(ctx, "id")
	if err != nil {
		t.Fatalf("ParseID returned error: %v", err)
	}
	if id != 123 {
		t.Fatalf("expected id=123, got %d", id)
	}

	// 空参数
	ctxEmpty := newTestContext(http.MethodGet, "/users")
	_, err = utils.ParseID(ctxEmpty, "id")
	if err == nil {
		t.Fatalf("expected error for empty id, got nil")
	}
	if code := errors.GetErrorCode(err); code != errors.ErrCodeInvalidInput {
		t.Fatalf("expected error code %s, got %s", errors.ErrCodeInvalidInput, code)
	}
}

func TestHttpUtils_ParsePagination(t *testing.T) {
	utils := &HttpUtils{}

	// 默认值
	ctxDefault := newTestContext(http.MethodGet, "/items")
	reqDefault, err := utils.ParsePagination(ctxDefault)
	if err != nil {
		t.Fatalf("ParsePagination default returned error: %v", err)
	}
	if reqDefault.Page != 1 || reqDefault.PageSize != 20 {
		t.Fatalf("expected page=1,page_size=20, got page=%d,page_size=%d", reqDefault.Page, reqDefault.PageSize)
	}

	// 带排序与自定义分页
	ctxCustom := newTestContext(http.MethodGet, "/items?page=2&page_size=10&sort_by=created_at&sort_dir=desc")
	reqCustom, err := utils.ParsePagination(ctxCustom)
	if err != nil {
		t.Fatalf("ParsePagination custom returned error: %v", err)
	}
	if reqCustom.Page != 2 || reqCustom.PageSize != 10 {
		t.Fatalf("expected page=2,page_size=10, got page=%d,page_size=%d", reqCustom.Page, reqCustom.PageSize)
	}
	if dir, ok := reqCustom.Sort["created_at"]; !ok || dir != "desc" {
		t.Fatalf("expected sort created_at=desc, got %v", reqCustom.Sort)
	}

	// 非法 page
	ctxBadPage := newTestContext(http.MethodGet, "/items?page=0")
	if _, err := utils.ParsePagination(ctxBadPage); err == nil {
		t.Fatalf("expected error for invalid page, got nil")
	}
}

func TestHttpUtils_WriteErrorResponse(t *testing.T) {
	utils := &HttpUtils{}

	ctx := newTestContext(http.MethodGet, "/")
	// 使用预定义错误，验证状态码和 payload
	err := utils.WriteErrorResponse(ctx, errors.ErrNotFound())
	if err != nil {
		t.Fatalf("WriteErrorResponse returned error: %v", err)
	}

	rec := ctx.writer.(*httptest.ResponseRecorder)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}

	var payload httpx.ErrorPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if payload.Code != string(errors.ErrCodeNotFound) {
		t.Fatalf("expected error code %s, got %s", errors.ErrCodeNotFound, payload.Code)
	}
	if payload.Message == "" {
		t.Fatalf("expected non-empty error message")
	}

	// 再次写入应被忽略（response_written 标记）
	if err := utils.WriteErrorResponse(ctx, errors.ErrInternal()); err != nil {
		t.Fatalf("second WriteErrorResponse returned error: %v", err)
	}
}

