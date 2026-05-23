package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gochen/api/rest/internal/testutil"
	appaudited "gochen/app/audited"
	"gochen/db/query"
	"gochen/domain/audited"
	"gochen/httpx"
	"gochen/httpx/nethttp"
)

type recordingAuditStoreArgs struct {
	lastEntityID string
	lastOffset   int
	lastLimit    int
}

// SaveAuditRecord 写入一条审计记录。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - value：参数值（具体语义见函数上下文）（类型：audited.AuditRecord）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (s *recordingAuditStoreArgs) SaveAuditRecord(context.Context, audited.AuditRecord) (int64, error) {
	return 0, nil
}

// ListAuditRecordsByEntity 从存储中查询数据。
//
// 参数：
// - _：上下文（用于取消、超时与链路信息）
// - entityID：对象/实体标识
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：audited.AuditRecord）
// - err：错误信息（nil 表示成功）
func (s *recordingAuditStoreArgs) ListAuditRecordsByEntity(_ context.Context, entityID string, offset, limit int) ([]audited.AuditRecord, error) {
	s.lastEntityID = entityID
	s.lastOffset = offset
	s.lastLimit = limit
	return []audited.AuditRecord{}, nil
}

// TestRouteBuilder_MaxBodySizeEnforced 验证 RouteBuilder MaxBodySizeEnforced。
func TestRouteBuilder_MaxBodySizeEnforced(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Body.MaxBodySize = 8
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["POST /items"]
	if !ok {
		t.Fatalf("expected POST handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/items", strings.NewReader(`{"id":123,"x":"yyyyyyyyyyyy"}`))
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d", w.Code)
	}
}

// TestRouteBuilder_QueryAllowlistApplied 验证 RouteBuilder QueryAllowlistApplied。
func TestRouteBuilder_QueryAllowlistApplied(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.AllowedFilterFields = []string{"name", "status"}
		cfg.Query.AllowedSortFields = []string{"created_at"}
		cfg.Query.AllowedFields = []string{"id", "name"}
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?filter=name:like:John&filter=status:eq:active&sorts=created_at:desc&fields=id,name", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if svc.lastQuery == nil {
		t.Fatalf("expected lastQuery to be captured")
	}

	if len(svc.lastQuery.Filters) != 2 {
		t.Fatalf("expected 2 filter fields, got %d", len(svc.lastQuery.Filters))
	}
	if !hasFilter(svc.lastQuery.Filters, "name", query.FilterOpLike, "John") {
		t.Fatalf("expected filter name:like:John, got %+v", svc.lastQuery.Filters)
	}
	if !hasFilter(svc.lastQuery.Filters, "status", query.FilterOpEq, "active") {
		t.Fatalf("expected filter status:eq:active, got %+v", svc.lastQuery.Filters)
	}

	if len(svc.lastQuery.Sorts) != 1 {
		t.Fatalf("expected 1 sort, got %d: %+v", len(svc.lastQuery.Sorts), svc.lastQuery.Sorts)
	}
	if svc.lastQuery.Sorts[0].Field != "created_at" || svc.lastQuery.Sorts[0].Direction != query.DESC {
		t.Fatalf("expected sort created_at=desc, got %+v", svc.lastQuery.Sorts[0])
	}

	if strings.Join(svc.lastQuery.Fields, ",") != "id,name" {
		t.Fatalf("expected fields id,name, got %v", svc.lastQuery.Fields)
	}
}

// TestRouteBuilder_QueryMultiSortOrderPreserved 验证 RouteBuilder QueryMultiSortOrderPreserved。
func TestRouteBuilder_QueryMultiSortOrderPreserved(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.AllowedSortFields = []string{"created_at", "id"}
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?sorts=created_at:desc,id:asc", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if svc.lastQuery == nil {
		t.Fatalf("expected lastQuery to be captured")
	}
	if len(svc.lastQuery.Sorts) != 2 {
		t.Fatalf("expected 2 sorts, got %d: %+v", len(svc.lastQuery.Sorts), svc.lastQuery.Sorts)
	}
	if svc.lastQuery.Sorts[0].Field != "created_at" || svc.lastQuery.Sorts[0].Direction != query.DESC {
		t.Fatalf("expected sort[0]=created_at:desc, got %+v", svc.lastQuery.Sorts[0])
	}
	if svc.lastQuery.Sorts[1].Field != "id" || svc.lastQuery.Sorts[1].Direction != query.ASC {
		t.Fatalf("expected sort[1]=id:asc, got %+v", svc.lastQuery.Sorts[1])
	}
}

// TestRouteBuilder_AuditTrailPaginationApplied 验证 RouteBuilder AuditTrailPaginationApplied。
func TestRouteBuilder_AuditTrailPaginationApplied(t *testing.T) {
	repo := auditedNoopRepo{}
	store := &recordingAuditStoreArgs{}
	app, err := appaudited.NewApplication(&repo, nil, nil, store)
	if err != nil {
		t.Fatalf("new audited app: %v", err)
	}

	builder, err := NewApiBuilder[*auditedFakeEntity, int64](app, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Audit.OperatorExtractor = func(httpx.IContext) (string, bool) { return "tester", true }
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items/:id/audit"]
	if !ok {
		t.Fatalf("expected GET handler for /items/:id/audit")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items/1/audit?page=2&size=20", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "1")

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if store.lastEntityID != "1" || store.lastOffset != 20 || store.lastLimit != 20 {
		t.Fatalf("unexpected audit args: entity_id=%q offset=%d limit=%d", store.lastEntityID, store.lastOffset, store.lastLimit)
	}
}

// TestRouteBuilder_Delete_RejectsPurgeQueryParam 验证 RouteBuilder Delete RejectsPurgeQueryParam。
func TestRouteBuilder_Delete_RejectsPurgeQueryParam(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["DELETE /items/:id"]
	if !ok {
		t.Fatalf("expected DELETE handler for /items/:id")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/items/1?purge=true", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "1")

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	if svc.lastCtx != nil {
		t.Fatalf("expected service not to be called when purge query param is present")
	}
}

// TestRouteBuilder_QueryDisallowedFilterReturnsInvalidInput 验证 RouteBuilder QueryDisallowedFilterReturnsInvalidInput。
func TestRouteBuilder_QueryDisallowedFilterReturnsInvalidInput(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.AllowedFilterFields = []string{"name", "status"}
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?filter=role:eq:admin", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	if svc.lastQuery != nil {
		t.Fatalf("expected service not to be called on invalid filter")
	}
}

// TestRouteBuilder_PaginationOptionsAllowlistApplied 验证 RouteBuilder PaginationOptionsAllowlistApplied。
func TestRouteBuilder_PaginationOptionsAllowlistApplied(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = true
		cfg.Query.AllowedFilterFields = []string{"name"}
		cfg.Query.AllowedSortFields = []string{"created_at"}
		cfg.Query.AllowedFields = []string{"id"}
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?page=2&size=10&filter=name:like:John&sorts=created_at:desc&fields=id", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if svc.lastPage == nil {
		t.Fatalf("expected lastPage to be captured")
	}
	if svc.lastPage.Page != 2 || svc.lastPage.Size != 10 {
		t.Fatalf("expected page=2 size=10, got page=%d size=%d", svc.lastPage.Page, svc.lastPage.Size)
	}
	if len(svc.lastPage.Filters) != 1 {
		t.Fatalf("expected 1 filter field, got %d", len(svc.lastPage.Filters))
	}
	if !hasFilter(svc.lastPage.Filters, "name", query.FilterOpLike, "John") {
		t.Fatalf("expected filter name:like:John, got %+v", svc.lastPage.Filters)
	}
	if len(svc.lastPage.Sorts) != 1 {
		t.Fatalf("expected 1 sort, got %d: %+v", len(svc.lastPage.Sorts), svc.lastPage.Sorts)
	}
	if svc.lastPage.Sorts[0].Field != "created_at" || svc.lastPage.Sorts[0].Direction != query.DESC {
		t.Fatalf("expected sort created_at=desc, got %+v", svc.lastPage.Sorts[0])
	}
	if strings.Join(svc.lastPage.Fields, ",") != "id" {
		t.Fatalf("expected fields id, got %v", svc.lastPage.Fields)
	}
}

// TestRouteBuilder_QueryDisallowedSortReturnsInvalidInput 验证 RouteBuilder QueryDisallowedSortReturnsInvalidInput。
func TestRouteBuilder_QueryDisallowedSortReturnsInvalidInput(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.AllowedSortFields = []string{"created_at"}
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?sorts=role:asc", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	if svc.lastQuery != nil {
		t.Fatalf("expected service not to be called on invalid sort")
	}
}

// TestRouteBuilder_QueryDisallowedFieldsReturnsInvalidInput 验证 RouteBuilder QueryDisallowedFieldsReturnsInvalidInput。
func TestRouteBuilder_QueryDisallowedFieldsReturnsInvalidInput(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.AllowedFields = []string{"id"}
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?fields=id,name", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	if svc.lastQuery != nil {
		t.Fatalf("expected service not to be called on invalid fields")
	}
}

// TestRouteBuilder_QueryInvalidSortDirectionReturnsInvalidInput 验证 RouteBuilder QueryInvalidSortDirectionReturnsInvalidInput。
func TestRouteBuilder_QueryInvalidSortDirectionReturnsInvalidInput(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.AllowedSortFields = []string{"created_at"}
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?sorts=created_at:down", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	if svc.lastQuery != nil {
		t.Fatalf("expected service not to be called on invalid sort direction")
	}
}

// TestRouteBuilder_QuerySchemaNormalizesTypedFilters 验证 RouteBuilder QuerySchemaNormalizesTypedFilters。
func TestRouteBuilder_QuerySchemaNormalizesTypedFilters(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.QuerySchema = query.NewQuerySchema(
			query.BoolField("active"),
			query.TimeField("created_at", query.AllowSort()),
			query.StringField("id", query.AllowSelect()),
		)
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?filter=active:eq:TRUE&filter=created_at:gte:2026-03-27T10:00:00Z&sorts=created_at:desc&fields=id", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if svc.lastQuery == nil {
		t.Fatalf("expected lastQuery to be captured")
	}
	if !hasFilter(svc.lastQuery.Filters, "active", query.FilterOpEq, "true") {
		t.Fatalf("expected bool filter to normalize to true, got %+v", svc.lastQuery.Filters)
	}
	if !hasFilter(svc.lastQuery.Filters, "created_at", query.FilterOpGte, "2026-03-27T10:00:00Z") {
		t.Fatalf("expected time filter to stay normalized RFC3339, got %+v", svc.lastQuery.Filters)
	}
	if len(svc.lastQuery.Filters) != 2 {
		t.Fatalf("expected 2 filter fields, got %+v", svc.lastQuery.Filters)
	}
	activeFilters := svc.lastQuery.Filters.Get("active")
	if len(activeFilters) != 1 || !activeFilters[0].Value.Bool {
		t.Fatalf("expected decoded bool filter to be true, got %+v", activeFilters)
	}
	createdFilters := svc.lastQuery.Filters.Get("created_at")
	if len(createdFilters) != 1 || createdFilters[0].Value.Time.IsZero() {
		t.Fatalf("expected decoded time filter to be populated, got %+v", createdFilters)
	}
}

// TestRouteBuilder_PaginationQuerySchemaDecodesTypedFilters 验证 RouteBuilder PaginationQuerySchemaDecodesTypedFilters。
func TestRouteBuilder_PaginationQuerySchemaDecodesTypedFilters(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = true
		cfg.Query.QuerySchema = query.NewQuerySchema(
			query.BoolField("active"),
		)
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?page=1&size=10&filter=active:eq:true", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if svc.lastPage == nil {
		t.Fatalf("expected pagination options to be captured")
	}
	activeFilters := svc.lastPage.Filters.Get("active")
	if len(activeFilters) != 1 || !activeFilters[0].Value.Bool {
		t.Fatalf("expected decoded pagination filter to be true, got %+v", activeFilters)
	}
}

// TestRouteBuilder_QuerySchemaRejectsInvalidTypedFilter 验证 RouteBuilder QuerySchemaRejectsInvalidTypedFilter。
func TestRouteBuilder_QuerySchemaRejectsInvalidTypedFilter(t *testing.T) {
	svc := newStubAppService(nil)
	builder, err := NewApiBuilder[*fakeEntity, int64](svc, nil)
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Query.EnablePagination = false
		cfg.Query.QuerySchema = query.NewQuerySchema(
			query.BoolField("active"),
		)
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.Handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items?filter=active:like:TRUE", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
	if svc.lastQuery != nil {
		t.Fatalf("expected service not to be called on invalid typed filter")
	}
}

// hasFilter 执行对应操作。
//
// 参数：
// - field：参数值（具体语义见函数上下文）（类型：string）
// - op：操作对象（类型：query.FilterOp）
// - value：值（待写入/比较/校验的值）（类型：string）
//
// 返回：
// - result：是否满足条件
func hasFilter(filters query.QueryFilters, field string, op query.FilterOp, value string) bool {
	for _, f := range filters.Get(field) {
		if f.Op == op && f.Value.Normalized == value {
			return true
		}
	}
	return false
}
