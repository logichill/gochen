package crud_test

import (
	"context"
	domcrud "gochen/domain/crud"
	"strconv"
	"testing"

	"gochen/app"
	domaudited "gochen/audit"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

const (
	actionCreate  = "create"
	actionUpdate  = "update"
	actionDelete  = "delete"
	actionRestore = "restore"
	actionPurge   = "purge"
)

type auditDoc struct {
	domcrud.TrackedEntity[int64]
	Name   string `json:"name"`
	State  string `json:"state"`
	Tenant string `json:"tenant,omitempty" dim:"tenant"`
	Scope  int64  `json:"scope,omitempty" dim:"scope"`
	Owner  string `json:"owner,omitempty" dim:"owner"`
	Extra  any    `json:"extra,omitempty"`
}

func auditSnapshot(t *testing.T, tenant string, scope int64, owner string) domaudited.DimensionSnapshot {
	t.Helper()
	metadata, err := meta.Describe[*auditDoc]()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domaudited.NewDimensionSnapshot(metadata, map[meta.Dimension]any{"tenant": tenant, "scope": scope, "owner": owner})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func auditTenant(record domaudited.AuditRecord) string {
	value, _ := record.Dimensions.Value("tenant")
	return value.(string)
}
func auditScope(record domaudited.AuditRecord) int64 {
	value, _ := record.Dimensions.Value("scope")
	return value.(int64)
}
func auditOwner(record domaudited.AuditRecord) string {
	value, _ := record.Dimensions.Value("owner")
	return value.(string)
}

func newAuditRepo() *testkit.AuditedMemoryRepository[*auditDoc, int64] {
	repository, repositoryErr := testkit.NewAuditedMemoryRepository[*auditDoc, int64](testkit.NewInt64Sequence(1))
	if repositoryErr != nil {
		panic(repositoryErr)
	}
	return repository
}

func newAuditedApplication(
	t *testing.T,
	options testOptions[*auditDoc, int64],
) (*testApplication[*auditDoc, int64], *testkit.AuditedMemoryRepository[*auditDoc, int64]) {
	t.Helper()
	repo := newAuditRepo()
	return newAuditedApplicationWith(t, repo, repo, options)
}

func newAuditedApplicationWith(
	t *testing.T,
	repo *testkit.AuditedMemoryRepository[*auditDoc, int64],
	store app.IAuditRepository,
	options testOptions[*auditDoc, int64],
) (*testApplication[*auditDoc, int64], *testkit.AuditedMemoryRepository[*auditDoc, int64]) {
	t.Helper()
	if options.Resource == "" {
		options.Resource = "doc"
	}
	if options.Authorizer == nil {
		options.Authorizer = auth.Allow()
	}
	app, err := newTestApplication(repo, store, options)
	if err != nil {
		t.Fatalf("new audited application: %v", err)
	}
	return app, repo
}

func operatorContext(t *testing.T, operator string) context.Context {
	t.Helper()
	ctx, err := contextx.WithOperator(context.Background(), operator)
	if err != nil {
		t.Fatalf("with operator: %v", err)
	}
	return ctx
}

func mustCreateDoc(t *testing.T, app *testApplication[*auditDoc, int64], ctx context.Context, name string) *auditDoc {
	t.Helper()
	entity := &auditDoc{Name: name, State: "new"}
	if err := app.Create(ctx, entity); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return entity
}

func auditRecords(
	t *testing.T,
	repo *testkit.AuditedMemoryRepository[*auditDoc, int64],
	ctx context.Context,
	id int64,
) []domaudited.AuditRecord {
	t.Helper()
	records, err := repo.ListAuditRecords(ctx, strconv.FormatInt(id, 10), 0, 100, spec.All())
	if err != nil {
		t.Fatalf("list audit records: %v", err)
	}
	return records
}

func requireErrorCode(t *testing.T, err error, code errors.ErrorCode) {
	t.Helper()
	if !errors.Is(err, code) {
		t.Fatalf("error = %v, want code %v", err, code)
	}
}

// saveOnlyAuditStore 只实现写入端口，用于验证审计写入不强制查询能力。
type saveOnlyAuditStore struct{}

func (saveOnlyAuditStore) SaveAuditRecord(context.Context, domaudited.AuditRecord) (int64, error) {
	return 0, nil
}

func (saveOnlyAuditStore) DataSourceID() string { return testkit.MemoryDataSourceID }

// otherSourceAuditStore 声明了不同的数据源，用于验证 D17 构造期校验。
type otherSourceAuditStore struct{}

func (otherSourceAuditStore) SaveAuditRecord(context.Context, domaudited.AuditRecord) (int64, error) {
	return 0, nil
}

func (otherSourceAuditStore) ListAuditRecords(context.Context, string, int, int, spec.Spec) ([]domaudited.AuditRecord, error) {
	return nil, nil
}

func (otherSourceAuditStore) DataSourceID() string { return "other" }

// recordingAuditStore 统计单条/批量写入次数，其余行为委托给共享同一事务的仓储。
type recordingAuditStore struct {
	*testkit.AuditedMemoryRepository[*auditDoc, int64]
	singleCalls int
	bulkCalls   int
	lastBulk    int
}

func (s *recordingAuditStore) SaveAuditRecord(ctx context.Context, record domaudited.AuditRecord) (int64, error) {
	s.singleCalls++
	return s.AuditedMemoryRepository.SaveAuditRecord(ctx, record)
}

func (s *recordingAuditStore) SaveAuditRecords(ctx context.Context, records []domaudited.AuditRecord) error {
	s.bulkCalls++
	s.lastBulk = len(records)
	return s.AuditedMemoryRepository.SaveAuditRecords(ctx, records)
}

// failingAuditStore 写入必失败，但读取端口与仓储共享同一数据源。
type failingAuditStore struct {
	*testkit.AuditedMemoryRepository[*auditDoc, int64]
	err error
}

func (s *failingAuditStore) SaveAuditRecord(context.Context, domaudited.AuditRecord) (int64, error) {
	return 0, s.err
}

func (s *failingAuditStore) SaveAuditRecords(context.Context, []domaudited.AuditRecord) error {
	return s.err
}
