package crud

import (
	"context"
	"strconv"
	"testing"

	"gochen/app/operation"
	"gochen/contextx"
	"gochen/db/query"
	"gochen/domain"
	"gochen/errors"
	"gochen/validate"
)

type testEntity struct {
	id      int64
	version uint64
}

// GetID 返回当前值。
//
// 返回：
// - result：数量/计数
func (e testEntity) GetID() int64 { return e.id }

// GetVersion 返回当前值。
//
// 返回：
// - result：数量/计数
func (e testEntity) GetVersion() uint64 { return e.version }

var _ domain.IEntity[int64] = testEntity{}

type recordingRepo struct {
	lastListOffset int
	lastListLimit  int
	count          int64
	items          []testEntity
}

// Create 创建对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要创建的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Create(ctx context.Context, e testEntity) error { return nil }

// Update 更新对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要更新的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Update(ctx context.Context, e testEntity) error { return nil }

// Delete 删除实体并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Delete(ctx context.Context, id int64) error { return nil }

// Get 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Get(ctx context.Context, id int64) (testEntity, error) {
	return testEntity{}, errors.New("not implemented")
}

// List 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - offset：分页偏移量（从 0 开始）
// - limit：分页大小（最大返回条数）
//
// 返回：
// - result1：列表结果（元素类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) List(ctx context.Context, offset, limit int) ([]testEntity, error) {
	r.lastListOffset = offset
	r.lastListLimit = limit
	return r.items, nil
}

// Count 返回匹配条件的记录数。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Count(ctx context.Context) (int64, error) { return r.count, nil }

// Exists 判断对象是否存在。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (r *recordingRepo) Exists(ctx context.Context, id int64) (bool, error) {
	return false, nil
}

type queryRepo struct {
	recordingRepo
}

var _ query.IQueryableRepository[testEntity, int64] = (*queryRepo)(nil)

// Query 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：列表结果（元素类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *queryRepo) Query(ctx context.Context, opts query.QueryOptions) ([]testEntity, error) {
	return r.items, nil
}

// QueryOne 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *queryRepo) QueryOne(ctx context.Context, opts query.QueryOptions) (testEntity, error) {
	return testEntity{}, errors.New("not implemented")
}

// QueryCount 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *queryRepo) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	return r.count, nil
}

type capturingQueryRepo struct {
	recordingRepo
	lastQueryOpts query.QueryOptions
}

var _ query.IQueryableRepository[testEntity, int64] = (*capturingQueryRepo)(nil)

// Query 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：列表结果（元素类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *capturingQueryRepo) Query(ctx context.Context, opts query.QueryOptions) ([]testEntity, error) {
	r.lastQueryOpts = opts
	return r.items, nil
}

// QueryOne 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *capturingQueryRepo) QueryOne(ctx context.Context, opts query.QueryOptions) (testEntity, error) {
	return testEntity{}, errors.New("not implemented")
}

// QueryCount 从存储中查询对象。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - opts：可选参数/配置项
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (r *capturingQueryRepo) QueryCount(ctx context.Context, opts query.QueryOptions) (int64, error) {
	r.lastQueryOpts = opts
	return r.count, nil
}

type trackingValidator struct {
	err    error
	called int
}

func singleQueryFilter(field string, expr query.QueryExpr) query.QueryFilters {
	var filters query.QueryFilters
	return filters.Append(field, expr)
}

// Validate 校验输入是否满足约束。
//
// 参数：
// - v：参数值（具体语义见函数上下文）（类型：any）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (t *trackingValidator) Validate(any) error {
	t.called++
	return t.err
}

var _ validate.IValidator = (*trackingValidator)(nil)

// TestApplication_ListByQuery_FallbackToListWhenNoFiltersOrSorts 验证 Application ListByQuery FallbackToListWhenNoFiltersOrSorts。
func TestApplication_ListByQuery_FallbackToListWhenNoFiltersOrSorts(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 1}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2

	app, err := NewApplication(repo, nil, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	got, err := app.ListByQuery(context.Background(), &query.QueryRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].GetID() != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if repo.lastListOffset != 0 || repo.lastListLimit != 2 {
		t.Fatalf("unexpected List args: offset=%d limit=%d", repo.lastListOffset, repo.lastListLimit)
	}
}

func TestApplication_CreateWrappedByOperationRunner(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication(repo, nil, nil)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	spec := &operation.Spec{
		Type:           "entity.create",
		Mode:           operation.ModeInline,
		Resource:       &operation.Resource{Type: "entity"},
		AffectedScopes: []string{"entities:list"},
	}
	entity := testEntity{id: 12}
	result, err := operation.DefaultRunner().Execute(context.Background(), spec, func(ctx context.Context) (*operation.Result, error) {
		if err := app.Create(ctx, entity); err != nil {
			return nil, err
		}
		return operation.MergeResult(&operation.Result{
			Result: map[string]any{"entity_id": int64(12)},
		}, spec, &operation.Resource{ID: strconv.FormatInt(entity.GetID(), 10)}), nil
	})
	if err != nil {
		t.Fatalf("create wrapped by operation runner: %v", err)
	}
	if result.Operation.Type != "entity.create" {
		t.Fatalf("unexpected type: %s", result.Operation.Type)
	}
	if result.Operation.Status != operation.StatusSettled {
		t.Fatalf("unexpected status: %s", result.Operation.Status)
	}
	if result.Resource == nil || result.Resource.Type != "entity" || result.Resource.ID != "12" {
		t.Fatalf("unexpected resource: %+v", result.Resource)
	}
	if len(result.AffectedScopes) != 1 || result.AffectedScopes[0] != "entities:list" {
		t.Fatalf("unexpected affected scopes: %+v", result.AffectedScopes)
	}
}

// TestApplication_ListByQuery_FallbackToListWhenOnlyInvalidSorts 验证 Application ListByQuery FallbackToListWhenOnlyInvalidSorts。
func TestApplication_ListByQuery_FallbackToListWhenOnlyInvalidSorts(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 1}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2

	app, err := NewApplication(repo, nil, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	got, err := app.ListByQuery(context.Background(), &query.QueryRequest{
		Sorts: []query.Sort{
			{Field: " ", Direction: query.ASC},                   // 空 field
			{Field: "id", Direction: query.SortDirection("bad")}, // 非法方向
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].GetID() != 1 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if repo.lastListOffset != 0 || repo.lastListLimit != 2 {
		t.Fatalf("unexpected List args: offset=%d limit=%d", repo.lastListOffset, repo.lastListLimit)
	}
}

// TestApplication_ListByQuery_RequiresQueryableRepositoryWhenFiltersPresent 验证 Application ListByQuery RequiresQueryableRepositoryWhenFiltersPresent。
func TestApplication_ListByQuery_RequiresQueryableRepositoryWhenFiltersPresent(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{
		Filters: singleQueryFilter("name", query.QueryExpr{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeString, Normalized: "x", String: "x"},
		}),
	})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected UnsupportedOperation, got: %v", err)
	}
}

// TestApplication_ListPage_FallbackToListCountWithoutQueryableRepo 验证 Application ListPage FallbackToListCountWithoutQueryableRepo。
func TestApplication_ListPage_FallbackToListCountWithoutQueryableRepo(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 11}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2
	app, err := NewApplication(repo, nil, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	// Page=0 Size=0 将被 Validate 调整：Page=1 Size=10 并被 MaxPageSize 限制为 2。
	res, err := app.ListPage(context.Background(), &query.PaginationOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Page != 1 || res.Size != 2 || res.Total != 11 {
		t.Fatalf("unexpected page result: %+v", res)
	}
	if repo.lastListOffset != 0 || repo.lastListLimit != 2 {
		t.Fatalf("unexpected List args: offset=%d limit=%d", repo.lastListOffset, repo.lastListLimit)
	}
}

// TestApplication_ListPage_FallbackToListCountWhenOnlyInvalidSorts 验证 Application ListPage FallbackToListCountWhenOnlyInvalidSorts。
func TestApplication_ListPage_FallbackToListCountWhenOnlyInvalidSorts(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 11}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2
	app, err := NewApplication(repo, nil, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	res, err := app.ListPage(context.Background(), &query.PaginationOptions{
		Page: 1,
		Size: 10,
		Sorts: []query.Sort{
			{Field: " ", Direction: query.ASC},
			{Field: "id", Direction: query.SortDirection("bad")},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Page != 1 || res.Size != 2 || res.Total != 11 {
		t.Fatalf("unexpected page result: %+v", res)
	}
	if repo.lastListOffset != 0 || repo.lastListLimit != 2 {
		t.Fatalf("unexpected List args: offset=%d limit=%d", repo.lastListOffset, repo.lastListLimit)
	}
}

// TestApplication_ListPage_RequiresQueryableRepositoryWhenFiltersPresent 验证 Application ListPage RequiresQueryableRepositoryWhenFiltersPresent。
func TestApplication_ListPage_RequiresQueryableRepositoryWhenFiltersPresent(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListPage(context.Background(), &query.PaginationOptions{
		Page: 1,
		Size: 10,
		Filters: singleQueryFilter("name", query.QueryExpr{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeString, Normalized: "x", String: "x"},
		}),
	})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected UnsupportedOperation, got: %v", err)
	}
}

// TestApplication_ListPage_RequiresQueryableRepositoryWhenAdvancedFiltersPresent 验证 Application ListPage RequiresQueryableRepositoryWhenAdvancedFiltersPresent。
func TestApplication_ListPage_RequiresQueryableRepositoryWhenAdvancedFiltersPresent(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListPage(context.Background(), &query.PaginationOptions{
		Page: 1,
		Size: 10,
		Advanced: query.AdvancedFilters{
			Or: []query.OrCondition{
				{"status": "active"},
			},
		},
	})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected UnsupportedOperation, got: %v", err)
	}
}

// TestApplication_CountByQuery_RequiresQueryableRepositoryWhenFiltersPresent 验证 Application CountByQuery RequiresQueryableRepositoryWhenFiltersPresent。
func TestApplication_CountByQuery_RequiresQueryableRepositoryWhenFiltersPresent(t *testing.T) {
	repo := &recordingRepo{count: 123}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.CountByQuery(context.Background(), &query.QueryRequest{
		Filters: singleQueryFilter("name", query.QueryExpr{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeString, Normalized: "x", String: "x"},
		}),
	})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected UnsupportedOperation, got: %v", err)
	}
}

// TestApplication_CountByQuery_FallbackToCountWhenNoFilters 验证 Application CountByQuery FallbackToCountWhenNoFilters。
func TestApplication_CountByQuery_FallbackToCountWhenNoFilters(t *testing.T) {
	repo := &recordingRepo{count: 123}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	got, err := app.CountByQuery(context.Background(), &query.QueryRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 123 {
		t.Fatalf("expected 123, got %d", got)
	}
}

// TestApplication_ListByQuery_UsesQueryableRepoWhenAvailable 验证 Application ListByQuery UsesQueryableRepoWhenAvailable。
func TestApplication_ListByQuery_UsesQueryableRepoWhenAvailable(t *testing.T) {
	qr := &queryRepo{}
	qr.items = []testEntity{{id: 7}}
	app, err := NewApplication(qr, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	got, err := app.ListByQuery(context.Background(), &query.QueryRequest{
		Filters: singleQueryFilter("name", query.QueryExpr{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeString, Normalized: "x", String: "x"},
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].GetID() != 7 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestApplication_ListPage_UsesQueryableRepoThroughTenantAwareWrapper(t *testing.T) {
	repo := newMockTenantQueryableRepository()
	wrapper := NewTenantAwareWrapper[*testTenantUser, int64](repo)
	app, err := NewApplication[*testTenantUser, int64](wrapper, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	ctx, err := contextx.WithTenantID(context.Background(), "tenant-app")
	if err != nil {
		t.Fatalf("with tenant id: %v", err)
	}

	_, err = app.ListPage(ctx, &query.PaginationOptions{
		Page: 1,
		Size: 5,
		Filters: singleQueryFilter("name", query.QueryExpr{
			Op:    query.FilterOpEq,
			Value: query.StringValue("alice"),
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expr, ok := repo.lastOpts.Filters.First("tenant_id")
	if !ok {
		t.Fatalf("expected tenant filter injected, got: %+v", repo.lastOpts.Filters)
	}
	if expr.Value.String != "tenant-app" {
		t.Fatalf("unexpected tenant filter: %+v", expr)
	}
	nameExpr, ok := repo.lastOpts.Filters.First("name")
	if !ok {
		t.Fatalf("expected original name filter preserved, got: %+v", repo.lastOpts.Filters)
	}
	if nameExpr.Value.String != "alice" {
		t.Fatalf("unexpected name filter: %+v", nameExpr)
	}
}

// TestApplication_Create_ValidatesBeforePersist 验证 Application Create ValidatesBeforePersist。
func TestApplication_Create_ValidatesBeforePersist(t *testing.T) {
	repo := &recordingRepo{}
	v := &trackingValidator{err: errors.New("validator hit")}
	cfg := DefaultServiceConfig()
	cfg.AutoValidate = true

	app, err := NewApplication(repo, v, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if err := app.Create(context.Background(), testEntity{id: 1}); !errors.Is(err, v.err) {
		t.Fatalf("expected validator error, got %v", err)
	}
	if v.called != 1 {
		t.Fatalf("expected validator to be called once, got %d", v.called)
	}
}

// TestApplication_Update_ValidatesBeforePersist 验证 Application Update ValidatesBeforePersist。
func TestApplication_Update_ValidatesBeforePersist(t *testing.T) {
	repo := &recordingRepo{}
	v := &trackingValidator{err: errors.New("validator hit")}
	cfg := DefaultServiceConfig()
	cfg.AutoValidate = true

	app, err := NewApplication(repo, v, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	if err := app.Update(context.Background(), testEntity{id: 1}); !errors.Is(err, v.err) {
		t.Fatalf("expected validator error, got %v", err)
	}
	if v.called != 1 {
		t.Fatalf("expected validator to be called once, got %d", v.called)
	}
}

// TestApplication_ListByQuery_PassesFieldsToRequest 验证 Application ListByQuery PassesFieldsToRequest。
func TestApplication_ListByQuery_PassesFieldsToRequest(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}}}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{
		Fields: []string{"id", "name"},
		Filters: singleQueryFilter("name", query.QueryExpr{
			Op:    query.FilterOpLike,
			Value: query.QueryValue{Type: query.FieldTypeString, Normalized: "x", String: "x"},
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.lastQueryOpts.Fields) != 2 || repo.lastQueryOpts.Fields[0] != "id" || repo.lastQueryOpts.Fields[1] != "name" {
		t.Fatalf("expected query fields to be passed through, got %+v", repo.lastQueryOpts.Fields)
	}
}

// TestApplication_ListByQuery_PassesQueryFiltersToQueryOptions 验证 Application ListByQuery PassesQueryFiltersToQueryOptions。
func TestApplication_ListByQuery_PassesQueryFiltersToQueryOptions(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}}}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{Filters: singleQueryFilter("active", query.QueryExpr{
		Op:    query.FilterOpEq,
		Value: query.QueryValue{Type: query.FieldTypeBool, Normalized: "true", Bool: true},
	})})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	activeFilters := repo.lastQueryOpts.Filters.Get("active")
	if len(activeFilters) != 1 || !activeFilters[0].Value.Bool {
		t.Fatalf("expected query filters to be passed through, got %+v", repo.lastQueryOpts.Filters)
	}
}

// TestApplication_ListByQuery_NormalizesSorts 验证 Application ListByQuery NormalizesSorts。
func TestApplication_ListByQuery_NormalizesSorts(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}}}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{
		Sorts: []query.Sort{
			{Field: "  name  ", Direction: query.ASC},
			{Field: " ", Direction: query.ASC},
			{Field: "id", Direction: query.SortDirection("bad")},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.lastQueryOpts.Sorts) != 1 ||
		repo.lastQueryOpts.Sorts[0].Field != "name" ||
		repo.lastQueryOpts.Sorts[0].Direction != query.ASC {
		t.Fatalf("unexpected query sorts: %+v", repo.lastQueryOpts.Sorts)
	}
}

// TestApplication_ListPage_PassesFieldsToRequest 验证 Application ListPage PassesFieldsToRequest。
func TestApplication_ListPage_PassesFieldsToRequest(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}, count: 1}}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListPage(context.Background(), &query.PaginationOptions{
		Page:   1,
		Size:   10,
		Fields: []string{"id"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.lastQueryOpts.Fields) != 1 || repo.lastQueryOpts.Fields[0] != "id" {
		t.Fatalf("expected query fields to be passed through, got %+v", repo.lastQueryOpts.Fields)
	}
}

// TestApplication_ListPage_PassesQueryFiltersToQueryOptions 验证 Application ListPage PassesQueryFiltersToQueryOptions。
func TestApplication_ListPage_PassesQueryFiltersToQueryOptions(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}, count: 1}}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListPage(context.Background(), &query.PaginationOptions{
		Page: 1,
		Size: 10,
		Filters: singleQueryFilter("active", query.QueryExpr{
			Op:    query.FilterOpEq,
			Value: query.QueryValue{Type: query.FieldTypeBool, Normalized: "true", Bool: true},
		}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	activeFilters := repo.lastQueryOpts.Filters.Get("active")
	if len(activeFilters) != 1 || !activeFilters[0].Value.Bool {
		t.Fatalf("expected query filters to be passed through, got %+v", repo.lastQueryOpts.Filters)
	}
}

func TestApplication_ListPage_PassesAdvancedFiltersToQueryOptions(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}, count: 1}}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	request := &query.PaginationOptions{
		Page: 1,
		Size: 10,
		Advanced: query.AdvancedFilters{
			Or: []query.OrCondition{
				{"status": "active"},
			},
			DateRange: &query.DateRangeExpr{Start: "2026-04-01T00:00:00Z"},
		},
	}
	_, err = app.ListPage(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.lastQueryOpts.Advanced.Or) != 1 || repo.lastQueryOpts.Advanced.Or[0]["status"] != "active" {
		t.Fatalf("expected advanced OR filters to be passed through, got %+v", repo.lastQueryOpts.Advanced)
	}
	if repo.lastQueryOpts.Advanced.DateRange == nil || repo.lastQueryOpts.Advanced.DateRange.Start != "2026-04-01T00:00:00Z" {
		t.Fatalf("expected advanced date range to be passed through, got %+v", repo.lastQueryOpts.Advanced)
	}
}

// TestApplication_CountByQuery_PassesQueryFiltersToQueryOptions 验证 Application CountByQuery PassesQueryFiltersToQueryOptions。
func TestApplication_CountByQuery_PassesQueryFiltersToQueryOptions(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{count: 1}}
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.CountByQuery(context.Background(), &query.QueryRequest{Filters: singleQueryFilter("active", query.QueryExpr{
		Op:    query.FilterOpEq,
		Value: query.QueryValue{Type: query.FieldTypeBool, Normalized: "true", Bool: true},
	})})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	activeFilters := repo.lastQueryOpts.Filters.Get("active")
	if len(activeFilters) != 1 || !activeFilters[0].Value.Bool {
		t.Fatalf("expected query filters to be passed through, got %+v", repo.lastQueryOpts.Filters)
	}
}

// ============================================================================
// 批量操作测试（全有或全无事务语义）
// ============================================================================

// batchTestRepo 用于批量操作测试的仓储
type batchTestRepo struct {
	recordingRepo
	existingIDs map[int64]bool
	creates     []int64
	updates     []int64
	deletes     []int64
}

type batchTestRepoTxKey struct{}

type batchTestRepoTxState struct {
	existingIDs map[int64]bool
	creates     []int64
	updates     []int64
	deletes     []int64
}

// newBatchTestRepo result：返回的实例（类型：*batchTestRepo）。
//
// 返回：
func newBatchTestRepo() *batchTestRepo {
	return &batchTestRepo{
		existingIDs: make(map[int64]bool),
		creates:     make([]int64, 0),
		updates:     make([]int64, 0),
		deletes:     make([]int64, 0),
	}
}

// txState ctx：上下文（用于取消、超时与链路信息）。
//
// 参数：
//
// 返回：
// - result1：返回结果（类型：*batchTestRepoTxState）
// - result2：是否满足条件
func (r *batchTestRepo) txState(ctx context.Context) (*batchTestRepoTxState, bool) {
	v := ctx.Value(batchTestRepoTxKey{})
	if v == nil {
		return nil, false
	}
	tx, ok := v.(*batchTestRepoTxState)
	return tx, ok && tx != nil
}

// Create 创建对象并写入存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - e：要创建的实体
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Create(ctx context.Context, e testEntity) error {
	if tx, ok := r.txState(ctx); ok {
		tx.creates = append(tx.creates, e.id)
		tx.existingIDs[e.id] = true
		return nil
	}
	r.creates = append(r.creates, e.id)
	r.existingIDs[e.id] = true
	return nil
}

func (r *batchTestRepo) Update(ctx context.Context, e testEntity) error {
	if tx, ok := r.txState(ctx); ok {
		if !tx.existingIDs[e.id] {
			return errors.NewCode(errors.NotFound, "entity not found")
		}
		tx.updates = append(tx.updates, e.id)
		return nil
	}
	if !r.existingIDs[e.id] {
		return errors.NewCode(errors.NotFound, "entity not found")
	}
	r.updates = append(r.updates, e.id)
	return nil
}

// Delete 删除实体并同步到存储。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Delete(ctx context.Context, id int64) error {
	if tx, ok := r.txState(ctx); ok {
		if !tx.existingIDs[id] {
			return errors.NewCode(errors.NotFound, "entity not found")
		}
		tx.deletes = append(tx.deletes, id)
		delete(tx.existingIDs, id)
		return nil
	}
	if !r.existingIDs[id] {
		return errors.NewCode(errors.NotFound, "entity not found")
	}
	r.deletes = append(r.deletes, id)
	delete(r.existingIDs, id)
	return nil
}

// Exists 判断对象是否存在。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：是否满足条件
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Exists(ctx context.Context, id int64) (bool, error) {
	if tx, ok := r.txState(ctx); ok {
		return tx.existingIDs[id], nil
	}
	return r.existingIDs[id], nil
}

// Get 从存储中查询实体。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
// - id：对象/实体标识
//
// 返回：
// - result1：返回结果（类型：testEntity）
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Get(ctx context.Context, id int64) (testEntity, error) {
	if tx, ok := r.txState(ctx); ok {
		if !tx.existingIDs[id] {
			return testEntity{}, errors.NewCode(errors.NotFound, "entity not found")
		}
		return testEntity{id: id}, nil
	}
	if !r.existingIDs[id] {
		return testEntity{}, errors.NewCode(errors.NotFound, "entity not found")
	}
	return testEntity{id: id}, nil
}

// BeginTx 开启事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - result1：返回结果（类型：context.Context）
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) BeginTx(ctx context.Context) (contextx.TxScope, error) {
	existingIDs := make(map[int64]bool, len(r.existingIDs))
	for k, v := range r.existingIDs {
		existingIDs[k] = v
	}
	creates := append([]int64(nil), r.creates...)
	tx := &batchTestRepoTxState{
		existingIDs: existingIDs,
		creates:     creates,
		updates:     append([]int64(nil), r.updates...),
		deletes:     append([]int64(nil), r.deletes...),
	}
	txCtx := context.WithValue(ctx, batchTestRepoTxKey{}, tx)
	return contextx.NewTxScope(txCtx, true)
}

func (r *batchTestRepo) WithinTx(ctx context.Context, fn func(txCtx context.Context) error) error {
	return runWithTxLifecycle(ctx, r, fn)
}

// Commit 提交事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Commit(txScope contextx.TxScope) error {
	tx, ok := r.txState(txScope.Context())
	if !ok {
		return errors.NewCode(errors.Internal, "missing tx state")
	}
	r.existingIDs = tx.existingIDs
	r.creates = tx.creates
	r.updates = tx.updates
	r.deletes = tx.deletes
	return nil
}

// Rollback 回滚事务。
//
// 参数：
// - ctx：上下文（用于取消、超时与链路信息）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (r *batchTestRepo) Rollback(txScope contextx.TxScope) error {
	_ = txScope
	return nil
}

// selectiveValidator 选择性校验器：对特定 ID 的实体返回错误
type selectiveValidator struct {
	failIDs map[int64]bool
}

// Validate 校验输入是否满足约束。
//
// 参数：
// - entity：实体对象
//
// 返回：
// - err：错误信息（nil 表示成功）
func (v *selectiveValidator) Validate(entity any) error {
	if e, ok := entity.(testEntity); ok {
		if v.failIDs[e.id] {
			return errors.New("validation failed for entity")
		}
	}
	return nil
}

// TestApplication_CreateAll_AllOrNothing_ValidationFailure 验证 Application CreateAll AllOrNothing ValidationFailure。
func TestApplication_CreateAll_AllOrNothing_ValidationFailure(t *testing.T) {
	repo := newBatchTestRepo()
	validator := &selectiveValidator{failIDs: map[int64]bool{2: true}}
	cfg := DefaultServiceConfig()
	cfg.AutoValidate = true

	app, err := NewApplication(repo, validator, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	entities := []testEntity{
		{id: 1},
		{id: 2}, // 校验失败
		{id: 3},
	}
	writer := NewBatchWriter(app)

	err = writer.CreateAll(context.Background(), entities)
	if err == nil {
		t.Fatalf("expected error when validation fails, got nil")
	}

	// 验证没有任何实体被创建（全回滚）
	if len(repo.creates) != 0 {
		t.Fatalf("expected 0 creates (all-or-nothing), got %d", len(repo.creates))
	}
}

func TestApplication_Create_RollsBackWhenAfterCreateFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	app.SetHooks(&Hooks[testEntity, int64]{
		AfterCreate: func(ctx context.Context, entity testEntity) error {
			return errors.New("after create failed")
		},
	})

	err = app.Create(context.Background(), testEntity{id: 1})
	if err == nil {
		t.Fatalf("expected error when after create hook fails")
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Fatalf("expected create to rollback when after hook fails")
	}
	if len(repo.creates) != 0 {
		t.Fatalf("expected no committed creates, got %+v", repo.creates)
	}
}

func TestApplication_Update_RollsBackWhenAfterUpdateFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	app.SetHooks(&Hooks[testEntity, int64]{
		AfterUpdate: func(ctx context.Context, entity testEntity) error {
			return errors.New("after update failed")
		},
	})

	err = app.Update(context.Background(), testEntity{id: 1})
	if err == nil {
		t.Fatalf("expected error when after update hook fails")
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no committed updates, got %+v", repo.updates)
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatalf("expected entity to remain after update rollback")
	}
}

func TestApplication_Delete_RollsBackWhenAfterDeleteFailsOnTransactionalRepo(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	app.SetHooks(&Hooks[testEntity, int64]{
		AfterDelete: func(ctx context.Context, id int64) error {
			return errors.New("after delete failed")
		},
	})

	err = app.Delete(context.Background(), 1)
	if err == nil {
		t.Fatalf("expected error when after delete hook fails")
	}
	if len(repo.deletes) != 0 {
		t.Fatalf("expected no committed deletes, got %+v", repo.deletes)
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatalf("expected delete to rollback when after hook fails")
	}
}

func TestApplication_Create_PostCommitHookRunsAfterCommit(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app.SetHooks(&Hooks[testEntity, int64]{
		AfterCreate: func(ctx context.Context, entity testEntity) error {
			afterCreateSawCommitted = len(repo.creates) > 0
			return nil
		},
		PostCommitCreate: func(ctx context.Context, entity testEntity) error {
			postCommitSawCommitted = len(repo.creates) > 0
			return nil
		},
	})

	if err := app.Create(context.Background(), testEntity{id: 1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected AfterCreate to run before commit")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected PostCommitCreate to run after commit")
	}
}

func TestApplication_Create_ReturnsPostCommitErrorWithoutRollback(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	app.SetHooks(&Hooks[testEntity, int64]{
		PostCommitCreate: func(ctx context.Context, entity testEntity) error {
			return errors.New("post commit failed")
		},
	})

	err = app.Create(context.Background(), testEntity{id: 1})
	if err == nil {
		t.Fatalf("expected post-commit error")
	}
	exists, err := repo.Exists(context.Background(), 1)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatalf("expected create to remain committed when post-commit hook fails")
	}
}

func TestApplication_Create_PostCommitHookRequiresTransactionalRepository(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	app.SetHooks(&Hooks[testEntity, int64]{
		PostCommitCreate: func(ctx context.Context, entity testEntity) error {
			return nil
		},
	})

	err = app.Create(context.Background(), testEntity{id: 1})
	if !errors.Is(err, errors.Unsupported) {
		t.Fatalf("expected Unsupported when post-commit hook is used without transaction support, got %v", err)
	}
}

// TestApplication_CreateAll_AllOrNothing_Success 验证 Application CreateAll AllOrNothing Success。
func TestApplication_CreateAll_AllOrNothing_Success(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	entities := []testEntity{{id: 1}, {id: 2}, {id: 3}}
	writer := NewBatchWriter(app)

	err = writer.CreateAll(context.Background(), entities)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证所有实体都被创建
	if len(repo.creates) != 3 {
		t.Fatalf("expected 3 creates, got %d", len(repo.creates))
	}
}

func TestApplication_CreateAll_RunsBeforeAndValidateOncePerEntity(t *testing.T) {
	repo := newBatchTestRepo()
	validator := &trackingValidator{}
	app, err := NewApplication(repo, validator, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var beforeCalls int
	app.SetHooks(&Hooks[testEntity, int64]{
		BeforeCreate: func(ctx context.Context, entity testEntity) error {
			beforeCalls++
			return nil
		},
	})

	writer := NewBatchWriter(app)
	err = writer.CreateAll(context.Background(), []testEntity{{id: 1}, {id: 2}})
	if err != nil {
		t.Fatalf("create all: %v", err)
	}
	if beforeCalls != 2 {
		t.Fatalf("expected BeforeCreate once per entity, got %d", beforeCalls)
	}
	if validator.called != 2 {
		t.Fatalf("expected Validate once per entity, got %d", validator.called)
	}
}

func TestApplication_UpdateAll_RunsBeforeAndValidateOncePerEntity(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	repo.existingIDs[2] = true
	validator := &trackingValidator{}
	app, err := NewApplication(repo, validator, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var beforeCalls int
	app.SetHooks(&Hooks[testEntity, int64]{
		BeforeUpdate: func(ctx context.Context, entity testEntity) error {
			beforeCalls++
			return nil
		},
	})

	writer := NewBatchWriter(app)
	err = writer.UpdateAll(context.Background(), []testEntity{{id: 1}, {id: 2}})
	if err != nil {
		t.Fatalf("update all: %v", err)
	}
	if beforeCalls != 2 {
		t.Fatalf("expected BeforeUpdate once per entity, got %d", beforeCalls)
	}
	if validator.called != 2 {
		t.Fatalf("expected Validate once per entity, got %d", validator.called)
	}
}

func TestApplication_DeleteAll_RunsBeforeOncePerID(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	repo.existingIDs[2] = true
	app, err := NewApplication[testEntity, int64](repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var beforeCalls int
	app.SetHooks(&Hooks[testEntity, int64]{
		BeforeDelete: func(ctx context.Context, id int64) error {
			beforeCalls++
			return nil
		},
	})

	writer := NewBatchWriter(app)
	err = writer.DeleteAll(context.Background(), []int64{1, 2})
	if err != nil {
		t.Fatalf("delete all: %v", err)
	}
	if beforeCalls != 2 {
		t.Fatalf("expected BeforeDelete once per id, got %d", beforeCalls)
	}
}

func TestApplication_CreateAll_AfterHookRunsBeforeCommitAndPostCommitRunsAfterCommit(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var afterCreateSawCommitted bool
	var postCommitSawCommitted bool
	app.SetHooks(&Hooks[testEntity, int64]{
		AfterCreate: func(ctx context.Context, entity testEntity) error {
			if len(repo.creates) > 0 {
				afterCreateSawCommitted = true
			}
			return nil
		},
		PostCommitCreate: func(ctx context.Context, entity testEntity) error {
			if len(repo.creates) > 0 {
				postCommitSawCommitted = true
			}
			return nil
		},
	})

	writer := NewBatchWriter(app)
	if err := writer.CreateAll(context.Background(), []testEntity{{id: 1}, {id: 2}}); err != nil {
		t.Fatalf("create all: %v", err)
	}
	if afterCreateSawCommitted {
		t.Fatalf("expected AfterCreate to run before commit in batch path")
	}
	if !postCommitSawCommitted {
		t.Fatalf("expected PostCommitCreate to run after commit in batch path")
	}
}

func TestApplication_CreateAll_RollsBackWhenAfterCreateFails(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	var afterCalls int
	app.SetHooks(&Hooks[testEntity, int64]{
		AfterCreate: func(ctx context.Context, entity testEntity) error {
			afterCalls++
			if entity.id == 2 {
				return errors.New("after create failed")
			}
			return nil
		},
	})

	writer := NewBatchWriter(app)
	err = writer.CreateAll(context.Background(), []testEntity{{id: 1}, {id: 2}})
	if err == nil {
		t.Fatalf("expected after create failure")
	}
	if afterCalls != 2 {
		t.Fatalf("expected after hook to run for each created entity before failure, got %d", afterCalls)
	}
	if len(repo.creates) != 0 {
		t.Fatalf("expected batch create to rollback when after hook fails, got %+v", repo.creates)
	}
}

// TestApplication_DeleteAll_AllOrNothing_NotFoundFails 验证 Application DeleteAll AllOrNothing NotFoundFails。
func TestApplication_DeleteAll_AllOrNothing_NotFoundFails(t *testing.T) {
	repo := newBatchTestRepo()
	// 预置实体 1, 3
	repo.existingIDs[1] = true
	repo.existingIDs[3] = true

	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	writer := NewBatchWriter(app)

	// 尝试删除 1, 2, 3（其中 2 不存在）
	err = writer.DeleteAll(context.Background(), []int64{1, 2, 3})
	if err == nil {
		t.Fatalf("expected error when entity not found, got nil")
	}
	if !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected NotFound error, got: %v", err)
	}

	// 验证事务回滚：不应删除任何已存在实体
	if !repo.existingIDs[1] || !repo.existingIDs[3] {
		t.Fatalf("expected rollback to keep entities 1 and 3, got %+v", repo.existingIDs)
	}
}

// TestApplication_DeleteAll_AllOrNothing_Success 验证 Application DeleteAll AllOrNothing Success。
func TestApplication_DeleteAll_AllOrNothing_Success(t *testing.T) {
	repo := newBatchTestRepo()
	repo.existingIDs[1] = true
	repo.existingIDs[2] = true
	repo.existingIDs[3] = true

	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}
	writer := NewBatchWriter(app)

	err = writer.DeleteAll(context.Background(), []int64{1, 2, 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证所有实体都被删除
	if len(repo.existingIDs) != 0 {
		t.Fatalf("expected all entities deleted, got %d remaining", len(repo.existingIDs))
	}
}

// TestApplication_CreateAll_ExceedsMaxBatchSize 验证 Application CreateAll ExceedsMaxBatchSize。
func TestApplication_CreateAll_ExceedsMaxBatchSize(t *testing.T) {
	repo := newBatchTestRepo()
	cfg := DefaultServiceConfig()
	cfg.MaxBatchSize = 3

	app, err := NewApplication(repo, nil, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	entities := []testEntity{{id: 1}, {id: 2}, {id: 3}, {id: 4}}
	writer := NewBatchWriter(app)
	err = writer.CreateAll(context.Background(), entities)
	if !errors.Is(err, errors.Validation) {
		t.Fatalf("expected Validation error for exceeding max batch size, got: %v", err)
	}
}

// TestApplication_DeleteAll_ExceedsMaxBatchSize 验证 Application DeleteAll ExceedsMaxBatchSize。
func TestApplication_DeleteAll_ExceedsMaxBatchSize(t *testing.T) {
	repo := newBatchTestRepo()
	cfg := DefaultServiceConfig()
	cfg.MaxBatchSize = 3

	app, err := NewApplication(repo, nil, cfg)
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	writer := NewBatchWriter(app)
	err = writer.DeleteAll(context.Background(), []int64{1, 2, 3, 4})
	if !errors.Is(err, errors.Validation) {
		t.Fatalf("expected Validation error for exceeding max batch size, got: %v", err)
	}
}

// TestApplication_CreateAll_EmptyListReturnsNil 验证 Application CreateAll EmptyListReturnsNil。
func TestApplication_CreateAll_EmptyListReturnsNil(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	writer := NewBatchWriter(app)
	err = writer.CreateAll(context.Background(), []testEntity{})
	if err != nil {
		t.Fatalf("expected nil error for empty list, got: %v", err)
	}
}

// TestApplication_DeleteAll_EmptyListReturnsNil 验证 Application DeleteAll EmptyListReturnsNil。
func TestApplication_DeleteAll_EmptyListReturnsNil(t *testing.T) {
	repo := newBatchTestRepo()
	app, err := NewApplication(repo, nil, DefaultServiceConfig())
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	writer := NewBatchWriter(app)
	err = writer.DeleteAll(context.Background(), []int64{})
	if err != nil {
		t.Fatalf("expected nil error for empty list, got: %v", err)
	}
}
