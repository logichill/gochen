package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	app "gochen/app"
	"gochen/domain/entity"
	"gochen/domain/repository"
	httpx "gochen/http"
	hbasic "gochen/http/basic"
	validation "gochen/validation"
)

type fakeEntity struct {
	entity.EntityFields
}

func (f *fakeEntity) Validate() error { return nil }

type noopRepository struct{}

func (noopRepository) Create(context.Context, *fakeEntity) error           { return nil }
func (noopRepository) GetByID(context.Context, int64) (*fakeEntity, error) { return nil, nil }
func (noopRepository) Update(context.Context, *fakeEntity) error           { return nil }
func (noopRepository) Delete(context.Context, int64) error                 { return nil }
func (noopRepository) List(context.Context, int, int) ([]*fakeEntity, error) {
	return []*fakeEntity{}, nil
}
func (noopRepository) Count(context.Context) (int64, error)        { return 0, nil }
func (noopRepository) Exists(context.Context, int64) (bool, error) { return true, nil }

type trackingValidator struct {
	err    error
	called int
}

func (t *trackingValidator) Validate(any) error {
	t.called++
	return t.err
}

func TestRestfulBuilderAppliesServiceConfig(t *testing.T) {
	repo := &noopRepository{}
	validator := &trackingValidator{err: errors.New("validator hit")}

	svc := app.NewApplication[*fakeEntity](repo, nil, nil)
	builder := NewApiBuilder(svc, validator)

	builder.Service(func(cfg *app.ServiceConfig) {
		cfg.MaxBatchSize = 77
	})

	group := newMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if got := svc.GetConfig().MaxBatchSize; got != 77 {
		t.Fatalf("expected MaxBatchSize 77, got %d", got)
	}

	if err := svc.Validate(&fakeEntity{}); !errors.Is(err, validator.err) {
		t.Fatalf("expected validator error, got %v", err)
	}

	if validator.called != 1 {
		t.Fatalf("expected validator to be called once, got %d", validator.called)
	}
}

type stubAppService struct {
	cfg       *app.ServiceConfig
	validator validation.IValidator
	order     *[]string
}

func newStubAppService(order *[]string) *stubAppService {
	return &stubAppService{
		cfg:   cloneServiceConfig(app.DefaultServiceConfig()),
		order: order,
	}
}

func (s *stubAppService) Create(context.Context, *fakeEntity) error           { return nil }
func (s *stubAppService) GetByID(context.Context, int64) (*fakeEntity, error) { return nil, nil }
func (s *stubAppService) Update(context.Context, *fakeEntity) error           { return nil }
func (s *stubAppService) Delete(context.Context, int64) error                 { return nil }
func (s *stubAppService) List(context.Context, int, int) ([]*fakeEntity, error) {
	return []*fakeEntity{}, nil
}
func (s *stubAppService) Count(context.Context) (int64, error)                   { return 0, nil }
func (s *stubAppService) Repository() repository.IRepository[*fakeEntity, int64] { return nil }
func (s *stubAppService) ListByQuery(context.Context, *app.QueryParams) ([]*fakeEntity, error) {
	if s.order != nil {
		*s.order = append(*s.order, "handler-list")
	}
	return []*fakeEntity{}, nil
}
func (s *stubAppService) ListPage(context.Context, *app.PaginationOptions) (*app.PagedResult[*fakeEntity], error) {
	if s.order != nil {
		*s.order = append(*s.order, "handler")
	}
	return &app.PagedResult[*fakeEntity]{Data: []*fakeEntity{}, Total: 0, Page: 1, Size: 10}, nil
}
func (s *stubAppService) CountByQuery(context.Context, *app.QueryParams) (int64, error) {
	return 0, nil
}
func (s *stubAppService) CreateBatch(context.Context, []*fakeEntity) (*app.BatchOperationResult, error) {
	return &app.BatchOperationResult{}, nil
}
func (s *stubAppService) UpdateBatch(context.Context, []*fakeEntity) (*app.BatchOperationResult, error) {
	return &app.BatchOperationResult{}, nil
}
func (s *stubAppService) DeleteBatch(context.Context, []int64) (*app.BatchOperationResult, error) {
	return &app.BatchOperationResult{}, nil
}
func (s *stubAppService) Validate(entity *fakeEntity) error {
	if s.cfg != nil && s.cfg.AutoValidate && s.validator != nil {
		return s.validator.Validate(entity)
	}
	return nil
}
func (s *stubAppService) BeforeCreate(context.Context, *fakeEntity) error { return nil }
func (s *stubAppService) AfterCreate(context.Context, *fakeEntity) error  { return nil }
func (s *stubAppService) BeforeUpdate(context.Context, *fakeEntity) error { return nil }
func (s *stubAppService) AfterUpdate(context.Context, *fakeEntity) error  { return nil }
func (s *stubAppService) BeforeDelete(context.Context, int64) error       { return nil }
func (s *stubAppService) AfterDelete(context.Context, int64) error        { return nil }
func (s *stubAppService) GetConfig() *app.ServiceConfig                   { return s.cfg }
func (s *stubAppService) UpdateConfig(cfg *app.ServiceConfig)             { s.cfg = cloneServiceConfig(cfg) }
func (s *stubAppService) SetValidator(v validation.IValidator)            { s.validator = v }

func TestRouteBuilderMiddlewareChain(t *testing.T) {
	var order []string
	svc := newStubAppService(&order)
	builder := NewApiBuilder[*fakeEntity](svc, nil)

	builder.Middleware(func(ctx httpx.IHttpContext, next func() error) error {
		order = append(order, "builder")
		return next()
	})

	builder.Route(func(cfg *RouteConfig) {
		cfg.BasePath = "/items"
		cfg.Middlewares = append(cfg.Middlewares, func(ctx httpx.IHttpContext, next func() error) error {
			order = append(order, "route")
			return next()
		})
	})

	group := newMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler, ok := group.handlers["GET /items"]
	if !ok {
		t.Fatalf("expected GET handler for /items")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items", nil)
	ctx := hbasic.NewBaseHttpContext(w, r)
	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	expected := []string{"builder", "route", "handler"}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("unexpected middleware order: want %v, got %v", expected, order)
	}
}

type mockRouteGroup struct {
	handlers map[string]httpx.HttpHandler
}

func newMockRouteGroup() *mockRouteGroup {
	return &mockRouteGroup{
		handlers: make(map[string]httpx.HttpHandler),
	}
}

func (m *mockRouteGroup) key(method, path string) string {
	return method + " " + path
}

func (m *mockRouteGroup) GET(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	m.handlers[m.key("GET", path)] = handler
	return m
}

func (m *mockRouteGroup) POST(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	m.handlers[m.key("POST", path)] = handler
	return m
}

func (m *mockRouteGroup) PUT(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	m.handlers[m.key("PUT", path)] = handler
	return m
}

func (m *mockRouteGroup) DELETE(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	m.handlers[m.key("DELETE", path)] = handler
	return m
}

func (m *mockRouteGroup) PATCH(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	m.handlers[m.key("PATCH", path)] = handler
	return m
}

func (m *mockRouteGroup) HEAD(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	m.handlers[m.key("HEAD", path)] = handler
	return m
}

func (m *mockRouteGroup) OPTIONS(path string, handler httpx.HttpHandler) httpx.IRouteGroup {
	m.handlers[m.key("OPTIONS", path)] = handler
	return m
}

func (m *mockRouteGroup) Group(string) httpx.IRouteGroup {
	return m
}

func (m *mockRouteGroup) Use(...httpx.Middleware) httpx.IRouteGroup {
	return m
}

func TestServiceConfigOverride(t *testing.T) {
	var order []string
	svc := newStubAppService(&order)
	// 默认配置
	def := svc.GetConfig()
	if def == nil {
		t.Fatalf("default service config should not be nil")
	}
	// 设置一个明显的值以便断言
	def.AutoValidate = false
	def.MaxBatchSize = 10
	svc.UpdateConfig(def)

	builder := NewApiBuilder[*fakeEntity](svc, nil)
	// 通过 Service() 覆盖配置
	builder.Service(func(cfg *app.ServiceConfig) {
		cfg.AutoValidate = true
		cfg.MaxBatchSize = 99
	})

	group := newMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// 验证已应用覆盖配置
	cfg := svc.GetConfig()
	if cfg == nil {
		t.Fatalf("service config should not be nil after build")
	}
	if !cfg.AutoValidate || cfg.MaxBatchSize != 99 {
		t.Fatalf("service config override not applied, got: AutoValidate=%v, MaxBatchSize=%d", cfg.AutoValidate, cfg.MaxBatchSize)
	}
}
