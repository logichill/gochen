package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gochen/api/rest/internal/testutil"
	appcrud "gochen/app/crud"
	auth "gochen/auth"
	"gochen/contextx"
	"gochen/domain/access"
	"gochen/errors"
	"gochen/httpx/nethttp"
)

type writeConstraintValidatingRepo struct {
	err error
}

type boundaryAwareFakeRepo struct {
	entities            map[int64]*fakeEntity
	boundaries          map[int64]access.ResourceBoundary
	getCalls            int
	resolveCalls        int
	updateGuardedCalls  int
	deleteGuardedCalls  int
	deleteGuardedIDs    []int64
	updateGuardedEntity *fakeEntity
}

type constrainedOnlyBatchService struct {
	*appcrud.Application[*fakeEntity, int64]
	createBatchConstraint access.WriteConstraint
	updateBatchConstraint access.WriteConstraint
	deleteBatchConstraint access.WriteConstraint
	createConstraint      access.WriteConstraint
	updateConstraint      access.WriteConstraint
	deleteConstraint      access.WriteConstraint
	createCalls           int
	updateCalls           int
	deleteCalls           int
	createBatchCalls      int
	updateBatchCalls      int
	deleteBatchCalls      int
}

func (r *writeConstraintValidatingRepo) Create(context.Context, *fakeEntity) error { return nil }
func (r *writeConstraintValidatingRepo) Update(context.Context, *fakeEntity) error { return nil }
func (r *writeConstraintValidatingRepo) Delete(context.Context, int64) error       { return nil }
func (r *writeConstraintValidatingRepo) Get(context.Context, int64) (*fakeEntity, error) {
	return &fakeEntity{}, nil
}
func (r *writeConstraintValidatingRepo) ValidateWriteConstraintSupport() error { return r.err }

func (s *constrainedOnlyBatchService) CreateAllWithConstraint(ctx context.Context, entities []*fakeEntity, constraint access.WriteConstraint) error {
	_ = ctx
	_ = entities
	s.createBatchConstraint = constraint
	s.createBatchCalls++
	return nil
}

func (s *constrainedOnlyBatchService) CreateWithConstraint(ctx context.Context, entity *fakeEntity, constraint access.WriteConstraint) error {
	_ = ctx
	_ = entity
	s.createConstraint = constraint
	s.createCalls++
	return nil
}

func (s *constrainedOnlyBatchService) UpdateWithConstraint(ctx context.Context, entity *fakeEntity, constraint access.WriteConstraint) error {
	_ = ctx
	_ = entity
	s.updateConstraint = constraint
	s.updateCalls++
	return nil
}

func (s *constrainedOnlyBatchService) DeleteWithConstraint(ctx context.Context, id int64, constraint access.WriteConstraint) error {
	_ = ctx
	_ = id
	s.deleteConstraint = constraint
	s.deleteCalls++
	return nil
}

func (s *constrainedOnlyBatchService) UpdateAllWithConstraint(ctx context.Context, entities []*fakeEntity, constraint access.WriteConstraint) error {
	_ = ctx
	_ = entities
	s.updateBatchConstraint = constraint
	s.updateBatchCalls++
	return nil
}

func (s *constrainedOnlyBatchService) DeleteAllWithConstraint(ctx context.Context, ids []int64, constraint access.WriteConstraint) error {
	_ = ctx
	_ = ids
	s.deleteBatchConstraint = constraint
	s.deleteBatchCalls++
	return nil
}

func newBoundaryAwareFakeRepo() *boundaryAwareFakeRepo {
	return &boundaryAwareFakeRepo{
		entities:   map[int64]*fakeEntity{},
		boundaries: map[int64]access.ResourceBoundary{},
	}
}

func (r *boundaryAwareFakeRepo) Create(context.Context, *fakeEntity) error { return nil }

func (r *boundaryAwareFakeRepo) Update(context.Context, *fakeEntity) error { return nil }

func (r *boundaryAwareFakeRepo) Delete(context.Context, int64) error { return nil }

func (r *boundaryAwareFakeRepo) Get(ctx context.Context, id int64) (*fakeEntity, error) {
	r.getCalls++
	if !r.matchesScope(ctx, id) {
		return nil, errors.NewCode(errors.NotFound, "record not found")
	}
	entity, ok := r.entities[id]
	if !ok || entity == nil {
		return nil, errors.NewCode(errors.NotFound, "record not found")
	}
	cloned := *entity
	return &cloned, nil
}

func (r *boundaryAwareFakeRepo) ResolveResourceByID(ctx context.Context, id int64) (access.ResourceBoundary, error) {
	_ = ctx
	r.resolveCalls++
	resource, ok := r.boundaries[id]
	if !ok {
		return access.ResourceBoundary{}, errors.NewCode(errors.NotFound, "record not found")
	}
	return resource, nil
}

func (r *boundaryAwareFakeRepo) CreateWithConstraint(context.Context, *fakeEntity, access.WriteConstraint) error {
	return nil
}

func (r *boundaryAwareFakeRepo) UpdateWithConstraint(ctx context.Context, entity *fakeEntity, constraint access.WriteConstraint) error {
	if !r.matchesScope(ctx, entity.ID) {
		return errors.NewCode(errors.NotFound, "record not found")
	}
	r.updateGuardedCalls++
	cloned := *entity
	r.updateGuardedEntity = &cloned
	resource, err := constraint.RequireResource("fake", formatFakeID(entity.ID))
	if err != nil {
		return err
	}
	if resource.ResourceID != formatFakeID(entity.ID) {
		return errors.NewCode(errors.Forbidden, "unexpected constrained resource")
	}
	return nil
}

func (r *boundaryAwareFakeRepo) DeleteWithConstraint(ctx context.Context, id int64, constraint access.WriteConstraint) error {
	if !r.matchesScope(ctx, id) {
		return errors.NewCode(errors.NotFound, "record not found")
	}
	r.deleteGuardedCalls++
	r.deleteGuardedIDs = append(r.deleteGuardedIDs, id)
	resource, err := constraint.RequireResource("fake", formatFakeID(id))
	if err != nil {
		return err
	}
	if resource.ResourceID != formatFakeID(id) {
		return errors.NewCode(errors.Forbidden, "unexpected constrained resource")
	}
	return nil
}

func (r *boundaryAwareFakeRepo) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	return contextx.NewTxScope(ctx, true)
}

func (r *boundaryAwareFakeRepo) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	txScope, err := r.BeginTx(ctx)
	if err != nil {
		return err
	}
	txCtx := txScope.Context()
	if txCtx == nil {
		return errors.NewCode(errors.Internal, "BeginTx returned nil txCtx")
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = r.Rollback(txScope)
	}()
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := r.Commit(txScope); err != nil {
		if contextx.IsAfterCommitError(err) {
			committed = true
		}
		return err
	}
	committed = true
	return contextx.RunAfterCommit(txCtx)
}

func (r *boundaryAwareFakeRepo) Commit(contextx.TxScope) error   { return nil }
func (r *boundaryAwareFakeRepo) Rollback(contextx.TxScope) error { return nil }

func (r *boundaryAwareFakeRepo) matchesScope(ctx context.Context, id int64) bool {
	resource, ok := r.boundaries[id]
	if !ok {
		return false
	}
	scope, ok := access.DataScopeFromContext(ctx)
	if !ok {
		return false
	}
	if resource.ManagedScopeID == 0 {
		return true
	}
	for _, visibleScopeID := range scope.VisibleScopeIDs {
		if visibleScopeID == resource.ManagedScopeID {
			return true
		}
	}
	return scope.ActiveScopeID == resource.ManagedScopeID
}

func newFakeEntityAuthorizer(t *testing.T, eval func(context.Context, auth.Principal, string, []auth.Resource) (auth.AuthzDecision, error)) auth.IAuthorizer {
	t.Helper()
	authorizer, err := auth.NewAuthorizer(
		auth.EvaluatorFunc(eval),
		auth.TypedResourceResolver(func(target *fakeEntity) (auth.Resource, bool) {
			return auth.Resource{
				Kind:     "fake",
				ID:       formatFakeID(target.ID),
				Revision: formatFakeID(int64(target.Version)),
			}, true
		}),
	)
	if err != nil {
		t.Fatalf("NewAuthorizer returned error: %v", err)
	}
	return authorizer
}

func TestRouteBuilder_Create_AuthorizesAndUsesWriteConstraint(t *testing.T) {
	svc := newStubAppService(nil)
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if permission != "fake:create" {
			t.Fatalf("unexpected permission: %s", permission)
		}
		if principal.SubjectID != 99 {
			t.Fatalf("unexpected principal: %+v", principal)
		}
		if len(resources) != 1 || resources[0].Kind != "fake" {
			t.Fatalf("unexpected resources: %+v", resources)
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Create: "fake:create"}))
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

	handler := group.Handlers["POST /items"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"name":"demo"}`))
	r.Header.Set("Content-Type", "application/json")
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	withPrincipalContext(t, ctx)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if svc.createGuardedCalls != 1 {
		t.Fatalf("expected CreateWithConstraint called once, got %d", svc.createGuardedCalls)
	}
	if len(svc.createConstraint.Resources) != 1 || svc.createConstraint.Resources[0].Kind != "fake" {
		t.Fatalf("unexpected create constraint: %+v", svc.createConstraint)
	}
}

func TestRouteBuilder_Update_AuthorizesAndUsesWriteConstraint(t *testing.T) {
	svc := newStubAppService(nil)
	svc.loadedEntity = &fakeEntity{ID: 7, Version: 11, Name: "before"}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if permission != "fake:update" {
			t.Fatalf("unexpected permission: %s", permission)
		}
		if principal.SubjectID != 99 {
			t.Fatalf("unexpected principal: %+v", principal)
		}
		if len(resources) != 1 || resources[0].ID != "7" || resources[0].Revision != "11" {
			t.Fatalf("unexpected resources: %+v", resources)
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Update: "fake:update"}))
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

	handler := group.Handlers["PUT /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/items/7", strings.NewReader(`{"name":"after"}`))
	r.Header.Set("Content-Type", "application/json")
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "7")
	withPrincipalContext(t, ctx)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if svc.updateGuardedCalls != 1 {
		t.Fatalf("expected UpdateWithConstraint called once, got %d", svc.updateGuardedCalls)
	}
	if svc.lastUpdated == nil || svc.lastUpdated.ID != 7 || svc.lastUpdated.Name != "after" {
		t.Fatalf("unexpected updated entity: %+v", svc.lastUpdated)
	}
	if len(svc.updateConstraint.Resources) != 1 || svc.updateConstraint.Resources[0].Revision != "11" {
		t.Fatalf("unexpected update constraint: %+v", svc.updateConstraint)
	}
}

func TestRouteBuilder_Delete_AuthorizesAndUsesWriteConstraint(t *testing.T) {
	svc := newStubAppService(nil)
	svc.loadedEntity = &fakeEntity{ID: 5, Version: 3, Name: "demo"}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if permission != "fake:delete" {
			t.Fatalf("unexpected permission: %s", permission)
		}
		if len(resources) != 1 || resources[0].ID != "5" || resources[0].Revision != "3" {
			t.Fatalf("unexpected resources: %+v", resources)
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Delete: "fake:delete"}))
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

	handler := group.Handlers["DELETE /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/items/5", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "5")
	withPrincipalContext(t, ctx)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if svc.deleteGuardedCalls != 1 {
		t.Fatalf("expected DeleteWithConstraint called once, got %d", svc.deleteGuardedCalls)
	}
	if svc.lastDeletedID != 5 {
		t.Fatalf("expected delete id=5, got %d", svc.lastDeletedID)
	}
	if len(svc.deleteConstraint.Resources) != 1 || svc.deleteConstraint.Resources[0].Revision != "3" {
		t.Fatalf("unexpected delete constraint: %+v", svc.deleteConstraint)
	}
}

func TestRouteBuilder_Get_AuthorizationDenyStopsResponse(t *testing.T) {
	svc := newStubAppService(nil)
	svc.loadedEntity = &fakeEntity{ID: 9, Version: 1, Name: "demo"}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		return auth.DenyDecision("forbidden", resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Get: "fake:get"}))
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

	handler := group.Handlers["GET /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/items/9", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "9")
	withPrincipalContext(t, ctx)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", w.Code)
	}
}

func TestRouteBuilder_Build_FailsFastWhenRepoLacksWriteConstraintSupport(t *testing.T) {
	svc := newStubAppService(nil)
	svc.repository = &writeConstraintValidatingRepo{err: errors.NewCode(errors.Unsupported, "missing result support")}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Update: "fake:update"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
	})

	group := testutil.NewMockRouteGroup()
	err = builder.Build(group)
	if err == nil {
		t.Fatalf("expected build to fail")
	}
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func TestRouteBuilder_BatchRoutes_AuthorizeAndUseWriteConstraints(t *testing.T) {
	svc := newStubAppService(nil)
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if len(resources) != 1 {
			return auth.AllowDecision(resources...), nil
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Create: "fake:create"}))
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

	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Authorization.Permissions.Update = "fake:update"
		cfg.Authorization.Permissions.Delete = "fake:delete"
	})

	svc.loadedEntity = &fakeEntity{ID: 1, Version: 7, Name: "demo"}
	cases := []struct {
		name       string
		routeKey   string
		method     string
		target     string
		body       string
		wantStatus int
		assertCall func(t *testing.T)
	}{
		{
			name:       "create",
			routeKey:   "POST /items/batch",
			method:     http.MethodPost,
			target:     "/items/batch",
			body:       `[{"name":"demo"}]`,
			wantStatus: http.StatusOK,
			assertCall: func(t *testing.T) {
				t.Helper()
				if svc.createBatchGuardedCalls != 1 {
					t.Fatalf("expected CreateAllWithConstraint called once, got %d", svc.createBatchGuardedCalls)
				}
				if len(svc.createBatchConstraint.Resources) != 1 {
					t.Fatalf("expected single create batch constraint resource, got %+v", svc.createBatchConstraint)
				}
			},
		},
		{
			name:       "update",
			routeKey:   "PUT /items/batch",
			method:     http.MethodPut,
			target:     "/items/batch",
			body:       `[{"id":1,"version":7,"name":"demo"}]`,
			wantStatus: http.StatusOK,
			assertCall: func(t *testing.T) {
				t.Helper()
				if svc.updateBatchGuardedCalls != 1 {
					t.Fatalf("expected UpdateAllWithConstraint called once, got %d", svc.updateBatchGuardedCalls)
				}
				if len(svc.updateBatchConstraint.Resources) != 1 || svc.updateBatchConstraint.Resources[0].Revision != "7" {
					t.Fatalf("unexpected update batch constraint: %+v", svc.updateBatchConstraint)
				}
			},
		},
		{
			name:       "delete",
			routeKey:   "DELETE /items/batch",
			method:     http.MethodDelete,
			target:     "/items/batch",
			body:       `{"ids":[1]}`,
			wantStatus: http.StatusOK,
			assertCall: func(t *testing.T) {
				t.Helper()
				if svc.deleteBatchGuardedCalls != 1 {
					t.Fatalf("expected DeleteAllWithConstraint called once, got %d", svc.deleteBatchGuardedCalls)
				}
				if len(svc.deleteBatchConstraint.Resources) != 1 || svc.deleteBatchConstraint.Resources[0].Revision != "7" {
					t.Fatalf("unexpected delete batch constraint: %+v", svc.deleteBatchConstraint)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := group.Handlers[tc.routeKey]
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			ctx, err := nethttp.NewBaseContext(w, r)
			if err != nil {
				t.Fatalf("NewBaseContext returned error: %v", err)
			}
			withPrincipalContext(t, ctx)

			if err := handler(ctx); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if w.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, w.Code)
			}

			tc.assertCall(t)
		})
	}
}

func TestRouteBuilder_BatchRoutes_AllowConstrainedOnlyService(t *testing.T) {
	baseApp, err := appcrud.NewApplication[*fakeEntity, int64](&writeConstraintValidatingRepo{}, nil, appcrud.DefaultServiceConfig())
	if err != nil {
		t.Fatalf("NewApplication returned error: %v", err)
	}
	svc := &constrainedOnlyBatchService{Application: baseApp}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{
		Create: "fake:create",
		Update: "fake:update",
		Delete: "fake:delete",
	}))
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

	handler := group.Handlers["POST /items/batch"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/items/batch", strings.NewReader(`[{"name":"demo"}]`))
	r.Header.Set("Content-Type", "application/json")
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	withPrincipalContext(t, ctx)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if svc.createBatchCalls != 1 {
		t.Fatalf("expected constrained batch writer called once, got %d", svc.createBatchCalls)
	}
	if len(svc.createBatchConstraint.Resources) != 1 {
		t.Fatalf("unexpected create batch constraint: %+v", svc.createBatchConstraint)
	}
}

func TestRouteBuilder_Get_UsesResourceBoundaryFirstHop(t *testing.T) {
	repo := newBoundaryAwareFakeRepo()
	repo.entities[7] = &fakeEntity{ID: 7, Version: 3, Name: "demo"}
	repo.boundaries[7] = auth.ResourceBoundaryFromAuth(auth.Resource{Kind: "fake", ID: "7", ManagedScopeID: 202, Revision: "3"})
	svc, err := appcrud.NewApplication[*fakeEntity, int64](repo, nil, appcrud.DefaultServiceConfig())
	if err != nil {
		t.Fatalf("NewApplication returned error: %v", err)
	}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if len(resources) != 1 || resources[0].ManagedScopeID != 202 {
			t.Fatalf("unexpected resources: %+v", resources)
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Get: "fake:get"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) { cfg.Routing.BasePath = "/items" })
	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler := group.Handlers["GET /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/items/7", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "7")
	withPrincipalContext(t, ctx)
	withRequestDataScope(t, ctx, auth.DataScope{ActiveScopeID: 101, VisibleScopeIDs: []int64{101}, Mode: auth.ScopeModeScoped})

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if repo.resolveCalls != 1 || repo.getCalls != 1 {
		t.Fatalf("expected resolve/get called once, got resolve=%d get=%d", repo.resolveCalls, repo.getCalls)
	}
}

func TestRouteBuilder_Update_UsesResourceBoundaryFirstHop(t *testing.T) {
	repo := newBoundaryAwareFakeRepo()
	repo.entities[7] = &fakeEntity{ID: 7, Version: 3, Name: "before"}
	repo.boundaries[7] = auth.ResourceBoundaryFromAuth(auth.Resource{Kind: "fake", ID: "7", ManagedScopeID: 202, Revision: "3"})
	svc, err := appcrud.NewApplication[*fakeEntity, int64](repo, nil, appcrud.DefaultServiceConfig())
	if err != nil {
		t.Fatalf("NewApplication returned error: %v", err)
	}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if len(resources) != 1 || resources[0].ManagedScopeID != 202 {
			t.Fatalf("unexpected resources: %+v", resources)
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Update: "fake:update"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) { cfg.Routing.BasePath = "/items" })
	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler := group.Handlers["PUT /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/items/7", strings.NewReader(`{"version":3,"name":"after"}`))
	r.Header.Set("Content-Type", "application/json")
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "7")
	withPrincipalContext(t, ctx)
	withRequestDataScope(t, ctx, auth.DataScope{ActiveScopeID: 101, VisibleScopeIDs: []int64{101}, Mode: auth.ScopeModeScoped})

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if repo.resolveCalls != 1 || repo.getCalls != 1 || repo.updateGuardedCalls != 1 {
		t.Fatalf("unexpected call counts resolve=%d get=%d update=%d", repo.resolveCalls, repo.getCalls, repo.updateGuardedCalls)
	}
	if repo.updateGuardedEntity == nil || repo.updateGuardedEntity.Name != "after" {
		t.Fatalf("unexpected updated entity: %+v", repo.updateGuardedEntity)
	}
}

func TestRouteBuilder_Delete_UsesResolvedResourceForAuthorization(t *testing.T) {
	repo := newBoundaryAwareFakeRepo()
	repo.entities[7] = &fakeEntity{ID: 7, Version: 3, Name: "demo"}
	repo.boundaries[7] = auth.ResourceBoundaryFromAuth(auth.Resource{Kind: "fake", ID: "7", ManagedScopeID: 202, Revision: "3"})
	svc, err := appcrud.NewApplication[*fakeEntity, int64](repo, nil, appcrud.DefaultServiceConfig())
	if err != nil {
		t.Fatalf("NewApplication returned error: %v", err)
	}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if len(resources) != 1 || resources[0].ManagedScopeID != 202 {
			t.Fatalf("unexpected resources: %+v", resources)
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Delete: "fake:delete"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) { cfg.Routing.BasePath = "/items" })
	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler := group.Handlers["DELETE /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/items/7", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "7")
	withPrincipalContext(t, ctx)
	withRequestDataScope(t, ctx, auth.DataScope{ActiveScopeID: 101, VisibleScopeIDs: []int64{101}, Mode: auth.ScopeModeScoped})

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if repo.resolveCalls != 1 || repo.getCalls != 0 || repo.deleteGuardedCalls != 1 {
		t.Fatalf("unexpected call counts resolve=%d get=%d delete=%d", repo.resolveCalls, repo.getCalls, repo.deleteGuardedCalls)
	}
}

func TestRouteBuilder_DeleteBatch_UsesResolvedResourcesAndScopedWrites(t *testing.T) {
	repo := newBoundaryAwareFakeRepo()
	repo.entities[7] = &fakeEntity{ID: 7, Version: 3, Name: "one"}
	repo.entities[8] = &fakeEntity{ID: 8, Version: 4, Name: "two"}
	repo.boundaries[7] = auth.ResourceBoundaryFromAuth(auth.Resource{Kind: "fake", ID: "7", ManagedScopeID: 202, Revision: "3"})
	repo.boundaries[8] = auth.ResourceBoundaryFromAuth(auth.Resource{Kind: "fake", ID: "8", ManagedScopeID: 303, Revision: "4"})
	svc, err := appcrud.NewApplication[*fakeEntity, int64](repo, nil, appcrud.DefaultServiceConfig())
	if err != nil {
		t.Fatalf("NewApplication returned error: %v", err)
	}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		if len(resources) != 2 {
			t.Fatalf("unexpected resources: %+v", resources)
		}
		if resources[0].ManagedScopeID != 202 {
			t.Fatalf("unexpected first resource: %+v", resources[0])
		}
		if resources[1].ManagedScopeID != 303 {
			t.Fatalf("unexpected second resource: %+v", resources[1])
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Delete: "fake:delete"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Routing.EnableBatch = true
	})
	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler := group.Handlers["DELETE /items/batch"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/items/batch", strings.NewReader(`{"ids":[7,8]}`))
	r.Header.Set("Content-Type", "application/json")
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	withPrincipalContext(t, ctx)
	withRequestDataScope(t, ctx, auth.DataScope{ActiveScopeID: 101, VisibleScopeIDs: []int64{101}, Mode: auth.ScopeModeScoped})

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if repo.resolveCalls != 2 || repo.getCalls != 0 || repo.deleteGuardedCalls != 2 {
		t.Fatalf("unexpected call counts resolve=%d get=%d delete=%d", repo.resolveCalls, repo.getCalls, repo.deleteGuardedCalls)
	}
	if len(repo.deleteGuardedIDs) != 2 || repo.deleteGuardedIDs[0] != 7 || repo.deleteGuardedIDs[1] != 8 {
		t.Fatalf("unexpected delete ids: %+v", repo.deleteGuardedIDs)
	}
}

func TestRouteBuilder_Create_AuthorizationConfigPropagatesConsistency(t *testing.T) {
	svc := newStubAppService(nil)
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		eval, err := auth.EvalContextFromContext(ctx)
		if err != nil {
			t.Fatalf("EvalContextFromContext: %v", err)
		}
		if eval.Consistency != auth.ConsistencyModeBoundedStaleness {
			t.Fatalf("unexpected consistency: %s", eval.Consistency)
		}
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Create: "fake:create"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Authorization.Consistency = auth.ConsistencyModeBoundedStaleness
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler := group.Handlers["POST /items"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"name":"demo"}`))
	r.Header.Set("Content-Type", "application/json")
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	withPrincipalContext(t, ctx)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
}

func TestRouteBuilder_Authorize_PinsSnapshotAcrossRequest(t *testing.T) {
	svc := newStubAppService(nil)
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Get: "fake:get"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	routeBuilder := NewRouteBuilder[*fakeEntity, int64](svc).(*RouteBuilder[*fakeEntity, int64])
	routeBuilder.WithConfig(builder.routeConfig)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/items/1", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	withPrincipalContext(t, ctx)

	reqCtx := ctx.RequestContext()
	calls := 0
	derived, err := auth.WithSnapshotResolver(reqCtx, auth.SnapshotResolverFunc(func(ctx context.Context, eval auth.AuthzEvalContext) (auth.SnapshotResolution, error) {
		calls++
		return auth.SnapshotResolution{Version: "snap-fixed"}, nil
	}))
	if err != nil {
		t.Fatalf("WithSnapshotResolver returned error: %v", err)
	}
	ctx.SetContext(reqCtx.WithContext(derived))

	serviceCtx, err := routeBuilder.serviceContext(ctx)
	if err != nil {
		t.Fatalf("serviceContext returned error: %v", err)
	}
	serviceCtx, firstDecision, err := routeBuilder.authorize(ctx, serviceCtx, "fake:get", &fakeEntity{ID: 1})
	if err != nil {
		t.Fatalf("first authorize returned error: %v", err)
	}
	if firstDecision.SnapshotVersion != "snap-fixed" {
		t.Fatalf("expected first snapshot snap-fixed, got %q", firstDecision.SnapshotVersion)
	}

	reqEval, err := auth.EvalContextFromContext(ctx.RequestContext())
	if err != nil {
		t.Fatalf("EvalContextFromContext returned error: %v", err)
	}
	if reqEval.SnapshotVersion != "snap-fixed" {
		t.Fatalf("expected pinned request snapshot snap-fixed, got %q", reqEval.SnapshotVersion)
	}

	secondCtx, err := routeBuilder.serviceContext(ctx)
	if err != nil {
		t.Fatalf("serviceContext returned error: %v", err)
	}
	secondCtx, secondDecision, err := routeBuilder.authorize(ctx, secondCtx, "fake:get", &fakeEntity{ID: 1})
	if err != nil {
		t.Fatalf("second authorize returned error: %v", err)
	}
	if secondDecision.SnapshotVersion != "snap-fixed" {
		t.Fatalf("expected second snapshot snap-fixed, got %q", secondDecision.SnapshotVersion)
	}
	if calls != 1 {
		t.Fatalf("expected resolver called once, got %d", calls)
	}
	if snapshot, ok := auth.SnapshotVersionFromContext(serviceCtx); !ok || snapshot != "snap-fixed" {
		t.Fatalf("expected first ctx pinned snapshot, got %q (ok=%v)", snapshot, ok)
	}
	if snapshot, ok := auth.SnapshotVersionFromContext(secondCtx); !ok || snapshot != "snap-fixed" {
		t.Fatalf("expected second ctx pinned snapshot, got %q (ok=%v)", snapshot, ok)
	}
}

func TestRouteBuilder_Get_PinsSnapshotBeforeInitialRead(t *testing.T) {
	svc := newStubAppService(nil)
	svc.loadedEntity = &fakeEntity{ID: 3, Version: 1, Name: "demo"}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Get: "fake:get"}))
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

	handler := group.Handlers["GET /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/items/3", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "3")
	withPrincipalContext(t, ctx)

	reqCtx := ctx.RequestContext()
	calls := 0
	derived, err := auth.WithSnapshotResolver(reqCtx, auth.SnapshotResolverFunc(func(ctx context.Context, eval auth.AuthzEvalContext) (auth.SnapshotResolution, error) {
		calls++
		return auth.SnapshotResolution{Version: "snap-before-read"}, nil
	}))
	if err != nil {
		t.Fatalf("WithSnapshotResolver returned error: %v", err)
	}
	ctx.SetContext(reqCtx.WithContext(derived))

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if snapshot, ok := auth.SnapshotVersionFromContext(svc.lastCtx); !ok || snapshot != "snap-before-read" {
		t.Fatalf("expected service.Get ctx pinned snapshot, got %q (ok=%v)", snapshot, ok)
	}
	if snapshot, ok := auth.SnapshotVersionFromContext(ctx.RequestContext()); !ok || snapshot != "snap-before-read" {
		t.Fatalf("expected request ctx pinned snapshot, got %q (ok=%v)", snapshot, ok)
	}
	if calls != 1 {
		t.Fatalf("expected resolver called once, got %d", calls)
	}
}

func TestRouteBuilder_Get_FailsClosedWhenInitialSnapshotBindingFails(t *testing.T) {
	svc := newStubAppService(nil)
	svc.loadedEntity = &fakeEntity{ID: 4, Version: 1, Name: "demo"}
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Get: "fake:get"}))
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

	handler := group.Handlers["GET /items/:id"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/items/4", nil)
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	ctx.SetParam("id", "4")
	withPrincipalContext(t, ctx)

	reqCtx := ctx.RequestContext()
	derived, err := auth.WithSnapshotResolver(reqCtx, auth.SnapshotResolverFunc(func(ctx context.Context, eval auth.AuthzEvalContext) (auth.SnapshotResolution, error) {
		return auth.SnapshotResolution{}, errors.NewCode(errors.ServiceUnavailable, "snapshot backend unavailable")
	}))
	if err != nil {
		t.Fatalf("WithSnapshotResolver returned error: %v", err)
	}
	ctx.SetContext(reqCtx.WithContext(derived))

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", w.Code)
	}
	if svc.lastCtx != nil {
		t.Fatalf("expected service.Get not called when initial binding fails")
	}
}

func TestRouteBuilder_Create_HighRiskBoundedStalenessFailsClosed(t *testing.T) {
	svc := newStubAppService(nil)
	authorizer := newFakeEntityAuthorizer(t, func(ctx context.Context, principal auth.Principal, permission string, resources []auth.Resource) (auth.AuthzDecision, error) {
		return auth.AllowDecision(resources...), nil
	})

	builder, err := NewApiBuilder[*fakeEntity, int64](svc, WithAuthorization[*fakeEntity, int64](authorizer, CRUDPermissions{Create: "fake:create"}))
	if err != nil {
		t.Fatalf("NewApiBuilder returned error: %v", err)
	}
	builder.Route(func(cfg *RouteConfig[int64]) {
		cfg.Routing.BasePath = "/items"
		cfg.Authorization.Consistency = auth.ConsistencyModeBoundedStaleness
		cfg.Authorization.HighRisk = true
	})

	group := testutil.NewMockRouteGroup()
	if err := builder.Build(group); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	handler := group.Handlers["POST /items"]
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"name":"demo"}`))
	r.Header.Set("Content-Type", "application/json")
	ctx, err := nethttp.NewBaseContext(w, r)
	if err != nil {
		t.Fatalf("NewBaseContext returned error: %v", err)
	}
	withPrincipalContext(t, ctx)

	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", w.Code)
	}
}

func withRequestDataScope(t *testing.T, ctx *nethttp.Context, scope auth.DataScope) {
	t.Helper()
	reqCtx := ctx.RequestContext()
	derived, err := auth.WithDataScope(reqCtx, scope)
	if err != nil {
		t.Fatalf("WithDataScope: %v", err)
	}
	ctx.SetContext(reqCtx.WithContext(derived))
}

var _ access.IResourceBoundaryRepository[*fakeEntity, int64] = (*boundaryAwareFakeRepo)(nil)
