package app_test

import (
	"fmt"
	"testing"

	"gochen/app"
	"gochen/app/crud"
	"gochen/app/crud/audit"
	"gochen/app/eventsourced"
	"gochen/auth"

	domcrud "gochen/domain/crud"
	domes "gochen/domain/eventsourced"
	"gochen/errors"
	"gochen/testkit"
)

// 外部包直接使用公共配置和组合端口，不需要访问私有执行骨架。
func ExampleLimits() {
	limits := app.Limits{MaxBulkSize: 25}.Normalize()
	fmt.Println(limits.MaxBulkSize, limits.MaxPageSize)
	// Output: 25 1000
}

func TestLimitsNormalize(t *testing.T) {
	for _, input := range []app.Limits{{}, {MaxBulkSize: -1, MaxPageSize: -2}} {
		if got := input.Normalize(); got != (app.Limits{MaxBulkSize: 1000, MaxPageSize: 1000}) {
			t.Fatalf("Normalize(%+v) = %+v", input, got)
		}
	}
}

type note struct {
	domcrud.Entity[int64]
}

func ExampleIDescriptorProvider() {
	repository, err := testkit.NewMemoryRepository[*note, int64](nil)
	if err != nil {
		panic(err)
	}
	application, err := crud.NewApplication(repository, crud.Options[*note, int64]{DeleteMode: crud.DeletePurge,
		Resource: "note", Authorizer: auth.Allow(), Limits: app.Limits{MaxPageSize: 20},
	})
	if err != nil {
		panic(err)
	}
	var descriptor app.IDescriptorProvider = application
	fmt.Println(descriptor.Descriptor().Resource, application.MaxPageSize())
	// Output: note 20
}

var (
	_ app.ITransactionRunner  = (*testkit.MemoryRepository[*note, int64])(nil)
	_ app.IAuditRepository    = (*testkit.AuditedMemoryRepository[*domcrud.TrackedEntity[int64], int64])(nil)
	_ app.IDescriptorProvider = (*crud.Application[*note, int64])(nil)
	_                         = crud.Options[*domcrud.TrackedEntity[int64], int64]{DeleteMode: crud.DeletePurge, Projector: app.Projector[*domcrud.TrackedEntity[int64]](nil)}
	_                         = eventsourced.Options[*domes.EventSourcedAggregate[int64], int64]{Limits: app.Limits{MaxBulkSize: 10}}
)

// 覆盖数据源标识的测试实现仍完整满足公开组合端口。
type auditSource struct {
	app.IAuditRepository
	source string
}

func (s auditSource) DataSourceID() string { return s.source }

func TestAuditSourceValidationThroughPublicOptions(t *testing.T) {
	repository, err := testkit.NewAuditedMemoryRepository[*domcrud.TrackedEntity[int64], int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"", "other"} {
		t.Run(source, func(t *testing.T) {
			store := auditSource{IAuditRepository: repository, source: source}
			options := crud.Options[*domcrud.TrackedEntity[int64], int64]{DeleteMode: crud.DeletePurge, Resource: "note", Authorizer: auth.Allow(), Audit: store}
			if _, err := crud.NewApplication(repository, options); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("write application accepted invalid audit source: %v", err)
			}
			options.DeleteMode = 0
			if _, err := crud.NewReadApplication(repository, options); !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("read application accepted invalid audit source: %v", err)
			}
		})
	}
	var nilStore *testkit.AuditedMemoryRepository[*domcrud.TrackedEntity[int64], int64]
	if _, err := audit.NewReader[int64](nilStore, repository, audit.Options{Resource: "note", Authorizer: auth.Allow()}); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("audited application accepted typed nil audit store: %v", err)
	}
}
