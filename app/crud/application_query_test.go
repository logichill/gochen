package crud

import (
	"context"

	"testing"

	"gochen/app/query"

	"gochen/errors"
)

// TestApplication_ListByQuery_FallbackToListWhenNoFiltersOrSorts 验证 Application ListByQuery FallbackToListWhenNoFiltersOrSorts。
func TestApplication_ListByQuery_FallbackToListWhenNoFiltersOrSorts(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 1}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2

	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: cfg})
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

// TestApplication_ListByQuery_RejectsInvalidSortDirection 验证 ListByQuery 对非法排序方向 fail-fast。
func TestApplication_ListByQuery_RejectsInvalidSortDirection(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 1}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2

	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{
		Sorts: []query.Sort{
			{Field: "id", Direction: query.SortDirection("bad")}, // 非法方向
		},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}
}

func TestApplication_ListByQuery_RejectsEmptySortField(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}}}
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{
		Sorts: []query.Sort{
			{Field: " ", Direction: query.ASC},
		},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}
	if len(repo.lastQueryOpts.Sorts) != 0 {
		t.Fatalf("expected query repository not to receive partial sorts, got %+v", repo.lastQueryOpts.Sorts)
	}
}

func TestApplication_ListByQuery_RejectsMixedValidAndInvalidSortDirection(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}}}
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{
		Sorts: []query.Sort{
			{Field: "name", Direction: query.ASC},
			{Field: "id", Direction: query.SortDirection("bad")},
		},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}
	if len(repo.lastQueryOpts.Sorts) != 0 {
		t.Fatalf("expected query repository not to receive partial sorts, got %+v", repo.lastQueryOpts.Sorts)
	}
}

// TestApplication_ListByQuery_RequiresQueryableRepositoryWhenFiltersPresent 验证 Application ListByQuery RequiresQueryableRepositoryWhenFiltersPresent。
func TestApplication_ListByQuery_RequiresQueryableRepositoryWhenFiltersPresent(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: cfg})
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

// TestApplication_ListPage_RejectsInvalidSortDirection 验证 ListPage 对非法排序方向 fail-fast。
func TestApplication_ListPage_RejectsInvalidSortDirection(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 11}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListPage(context.Background(), &query.PaginationOptions{
		Page: 1,
		Size: 10,
		Sorts: []query.Sort{
			{Field: "id", Direction: query.SortDirection("bad")},
		},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}
}

func TestApplication_ListPage_RejectsEmptySortField(t *testing.T) {
	repo := &recordingRepo{items: []testEntity{{id: 1}}, count: 11}
	cfg := DefaultServiceConfig()
	cfg.MaxPageSize = 2
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: cfg})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListPage(context.Background(), &query.PaginationOptions{
		Page: 1,
		Size: 10,
		Sorts: []query.Sort{
			{Field: " ", Direction: query.ASC},
		},
	})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("expected InvalidInput, got: %v", err)
	}
}

// TestApplication_ListPage_RequiresQueryableRepositoryWhenFiltersPresent 验证 Application ListPage RequiresQueryableRepositoryWhenFiltersPresent。
func TestApplication_ListPage_RequiresQueryableRepositoryWhenFiltersPresent(t *testing.T) {
	repo := &recordingRepo{}
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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

func TestApplication_CountByQuery_IgnoresFieldsAndSortsForCount(t *testing.T) {
	repo := &recordingRepo{count: 123}
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	got, err := app.CountByQuery(context.Background(), &query.QueryRequest{
		Fields: []string{"name"},
		Sorts:  []query.Sort{{Field: "name", Direction: query.ASC}},
	})
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
	app, err := NewApplication(qr, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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

// TestApplication_ListByQuery_PassesFieldsToRequest 验证 Application ListByQuery PassesFieldsToRequest。
func TestApplication_ListByQuery_PassesFieldsToRequest(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}}}
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
	if err != nil {
		t.Fatalf("new application: %v", err)
	}

	_, err = app.ListByQuery(context.Background(), &query.QueryRequest{
		Sorts: []query.Sort{
			{Field: "  name  ", Direction: query.ASC},
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	if len(repo.lastCountOpts.Fields) != 0 {
		t.Fatalf("expected count options not to include projection fields, got %+v", repo.lastCountOpts.Fields)
	}
}

// TestApplication_ListPage_PassesQueryFiltersToQueryOptions 验证 Application ListPage PassesQueryFiltersToQueryOptions。
func TestApplication_ListPage_PassesQueryFiltersToQueryOptions(t *testing.T) {
	repo := &capturingQueryRepo{recordingRepo: recordingRepo{items: []testEntity{{id: 1}}, count: 1}}
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	app, err := NewApplication(repo, &Options[testEntity, int64]{Config: DefaultServiceConfig()})
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
	activeFilters := repo.lastCountOpts.Filters.Get("active")
	if len(activeFilters) != 1 || !activeFilters[0].Value.Bool {
		t.Fatalf("expected query filters to be passed through, got %+v", repo.lastCountOpts.Filters)
	}
	if len(repo.lastCountOpts.Fields) != 0 || len(repo.lastCountOpts.Sorts) != 0 {
		t.Fatalf("expected count options to ignore fields/sorts, got fields=%+v sorts=%+v", repo.lastCountOpts.Fields, repo.lastCountOpts.Sorts)
	}
}
