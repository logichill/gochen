package crud

import (
	"context"
	"reflect"
	"testing"

	domaudited "gochen/audit"
	"gochen/auth"
	"gochen/contextx"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

// Port projections intentionally omit capabilities. No no-op ports are fabricated.
type deletionBase[T domain.IEntity[int64]] struct{ Repository[T, int64] }
type purgePorts[T domain.IEntity[int64]] struct {
	Repository[T, int64]
	domcrud.IPurgeRepository[T, int64]
}
type deletedReadPorts[T domain.IEntity[int64]] struct {
	purgePorts[T]
	domcrud.IDeletedReadRepository[T, int64]
}

type deletionRequests struct {
	requests []auth.Request
	allow    string
}

func (p *deletionRequests) Vocabulary() auth.Vocabulary { return auth.Vocabulary{} }
func (p *deletionRequests) Authorize(_ context.Context, request auth.Request) (auth.Decision, error) {
	p.requests = append(p.requests, request)
	return auth.Decision{Allowed: p.allow == "" || request.Action == p.allow, Spec: spec.All()}, nil
}

func newDeletionRepo(t *testing.T) *testkit.AuditedMemoryRepository[*auditEntity, int64] {
	t.Helper()
	repo, err := testkit.NewAuditedMemoryRepository[*auditEntity, int64](testkit.NewInt64Sequence(1))
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestDeleteModeRequiresExplicitSafeConfiguration(t *testing.T) {
	ordinary := newTestRepo()
	for _, mode := range []DeleteMode{0, 77, DeleteSoft} {
		options := Options[*testEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: mode}
		if _, err := Describe(options, true); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("Describe mode %d: %v", mode, err)
		}
		if _, err := NewApplication(ordinary, options); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("New mode %d: %v", mode, err)
		}
	}
	for _, flags := range []Options[*testEntity, int64]{{EnableRestore: true}, {EnableDeletedQuery: true}} {
		flags.Resource = "document"
		flags.Authorizer = auth.Allow()
		flags.DeleteMode = DeletePurge
		if _, err := NewApplication(ordinary, flags); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("non-soft capability accepted: %v", err)
		}
		if _, err := Describe(flags, true); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("Describe accepted capability: %v", err)
		}
	}
	repo := newDeletionRepo(t)
	missing := deletionBase[*auditEntity]{repo}
	options := Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeletePurge}
	if _, err := NewApplication(missing, options); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("missing purge: %v", err)
	}
	purge := purgePorts[*auditEntity]{repo, repo}
	if _, err := NewApplication(purge, options); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("missing including-deleted read: %v", err)
	}
	ports := deletedReadPorts[*auditEntity]{purge, repo}
	app, err := NewApplication(ports, options)
	if err != nil {
		t.Fatalf("purge must not require restore/query: %v", err)
	}
	if _, err := app.Restore(t.Context(), 1); !errors.Is(err, errors.Unsupported) {
		t.Fatalf("restore enabled by repository methods: %v", err)
	}
	if _, err := app.ListDeleted(t.Context(), 0, 10); !errors.Is(err, errors.Unsupported) {
		t.Fatalf("deleted query enabled by repository methods: %v", err)
	}
	for _, restore := range []bool{true, false} {
		requested := options
		requested.EnableRestore = restore
		requested.EnableDeletedQuery = !restore
		if _, err := NewApplication(ports, requested); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("missing requested capability: %v", err)
		}
	}
	var nilPurge *testkit.MemoryRepository[*auditEntity, int64]
	if _, err := NewApplication(nilPurge, options); !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("typed nil purge: %v", err)
	}
}

func TestDeleteModeSoftIsFrozenAndNeverEscalates(t *testing.T) {
	repo := newDeletionRepo(t)
	options := Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeleteSoft, Audit: repo}
	app, err := NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	options.DeleteMode = DeletePurge
	options.EnableRestore = true
	options.EnableDeletedQuery = true
	ctx := newOperatorContext(t)
	entity := &auditEntity{Name: "keep until explicit purge"}
	if err := app.Create(ctx, entity); err != nil {
		t.Fatal(err)
	}
	if app.DeleteAction() != ActionDelete {
		t.Fatal("mode was not frozen")
	}
	if err := app.Delete(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := repo.GetWithDeleted(ctx, entity.ID, spec.All())
	if err != nil || !deleted.IsDeleted() {
		t.Fatalf("not soft deleted: %+v %v", deleted, err)
	}
	if err := app.Delete(ctx, entity.ID); !errors.Is(err, errors.NotFound) && !errors.Is(err, errors.Conflict) {
		t.Fatalf("repeat delete: %v", err)
	}
	again, err := repo.GetWithDeleted(ctx, entity.ID, spec.All())
	if err != nil || again.Version != deleted.Version {
		t.Fatalf("repeat soft delete purged/changed record: %+v %v", again, err)
	}
	if _, err := app.Restore(ctx, entity.ID); !errors.Is(err, errors.Unsupported) {
		t.Fatalf("options mutation enabled restore: %v", err)
	}
	if _, err := app.ListDeleted(ctx, 0, 10); !errors.Is(err, errors.Unsupported) {
		t.Fatalf("options mutation enabled query: %v", err)
	}
	records, err := repo.ListAuditRecords(ctx, "1", 0, 10, spec.All())
	if err != nil || len(records) != 2 || records[1].Action != ActionDelete {
		t.Fatalf("soft audit: %+v %v", records, err)
	}
	if err := app.Purge(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetWithDeleted(ctx, entity.ID, spec.All()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("explicit purge failed: %v", err)
	}
}

func TestHardDeleteUsesPurgePipelineForLiveAndPreviouslyDeleted(t *testing.T) {
	repo := newDeletionRepo(t)
	ctx := newOperatorContext(t)
	soft, err := NewApplication(repo, Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeleteSoft, Audit: repo})
	if err != nil {
		t.Fatal(err)
	}
	live, old := &auditEntity{Name: "live"}, &auditEntity{Name: "old"}
	for _, entity := range []*auditEntity{live, old} {
		if err := soft.Create(ctx, entity); err != nil {
			t.Fatal(err)
		}
	}
	if err := soft.Delete(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	requests := &deletionRequests{allow: ActionPurge}
	stages := []string{}
	options := Options[*auditEntity, int64]{Resource: "document", Authorizer: requests, DeleteMode: DeletePurge, Audit: repo, Hooks: Hooks[*auditEntity]{ActionPurge: {
		Before: func(context.Context, *auditEntity) error { stages = append(stages, "before"); return nil },
		After:  func(context.Context, *auditEntity) error { stages = append(stages, "after"); return nil },
		PostCommit: func(ctx context.Context, _ *auditEntity) error {
			if _, active := contextx.TxLifecycleFromContext(ctx); active {
				t.Fatal("postcommit received tx")
			}
			stages = append(stages, "commit")
			return nil
		},
	}}}
	hard, err := NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if hard.DeleteAction() != ActionPurge {
		t.Fatal("wrong effective action")
	}
	if _, ok := hard.Descriptor().Action(ActionDelete); ok {
		t.Fatal("hard descriptor advertises delete")
	}
	described, err := Describe(options, true)
	if err != nil || !reflect.DeepEqual(described, hard.Descriptor()) {
		t.Fatalf("descriptor drift: %v", err)
	}
	if err := hard.DeleteBulk(ctx, []int64{live.ID, old.ID}); err != nil {
		t.Fatal(err)
	}
	if len(requests.requests) != 1 || requests.requests[0].Action != ActionPurge || requests.requests[0].Permission != "document:api:purge" {
		t.Fatalf("wrong authorization: %+v", requests.requests)
	}
	if !reflect.DeepEqual(stages, []string{"before", "after", "before", "after", "commit", "commit"}) {
		t.Fatalf("purge hooks: %v", stages)
	}
	for _, entity := range []*auditEntity{live, old} {
		if _, err := repo.GetWithDeleted(ctx, entity.ID, spec.All()); !errors.Is(err, errors.NotFound) {
			t.Fatalf("not physically removed: %v", err)
		}
		id := "1"
		if entity == old {
			id = "2"
		}
		records, err := repo.ListAuditRecords(ctx, id, 0, 10, spec.All())
		if err != nil || len(records) < 2 || records[len(records)-1].Action != ActionPurge {
			t.Fatalf("independent audit after purge: %+v %v", records, err)
		}
	}
}

func TestHardDeleteDeniedByDeleteOnlyAndRollbackUsesPurge(t *testing.T) {
	repo := newDeletionRepo(t)
	ctx := newOperatorContext(t)
	entity := &auditEntity{Name: "must survive"}
	if err := repo.Create(ctx, entity, spec.All()); err != nil {
		t.Fatal(err)
	}
	policy := &deletionRequests{allow: ActionDelete}
	called := false
	options := Options[*auditEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: DeletePurge, Hooks: Hooks[*auditEntity]{ActionPurge: {Before: func(context.Context, *auditEntity) error { called = true; return nil }}}}
	app, err := NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Delete(ctx, entity.ID); !errors.Is(err, errors.Forbidden) || called {
		t.Fatalf("delete-only permission allowed hard delete: %v", err)
	}
	if _, err := repo.Get(ctx, entity.ID, spec.All()); err != nil {
		t.Fatal(err)
	}
	policy.allow = ActionPurge
	if err := app.DeleteBulk(ctx, []int64{entity.ID, 999}); !errors.Is(err, errors.NotFound) {
		t.Fatalf("expected atomic failure: %v", err)
	}
	if _, err := repo.Get(ctx, entity.ID, spec.All()); err != nil {
		t.Fatalf("partial purge committed: %v", err)
	}
}

func TestHardModeCanManageHistoricalSoftRecordsIndependently(t *testing.T) {
	repo := newDeletionRepo(t)
	ctx := t.Context()
	entity := &auditEntity{Name: "old"}
	if err := repo.Create(ctx, entity, spec.All()); err != nil {
		t.Fatal(err)
	}
	soft, err := NewApplication(repo, Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeleteSoft})
	if err != nil {
		t.Fatal(err)
	}
	if err := soft.Delete(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(repo, Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeletePurge, EnableRestore: true, EnableDeletedQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := app.ListDeleted(ctx, 0, 10)
	if err != nil || len(deleted) != 1 {
		t.Fatalf("historical query: %+v %v", deleted, err)
	}
	restored, err := app.Restore(ctx, entity.ID)
	if err != nil || restored.IsDeleted() {
		t.Fatalf("historical restore: %+v %v", restored, err)
	}
	if err := app.Delete(ctx, entity.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetWithDeleted(ctx, entity.ID, spec.All()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("hard mode changed after restore: %v", err)
	}
}

func TestLifecycleActionsCannotBecomeGenericCommands(t *testing.T) {
	repo := newDeletionRepo(t)
	policy := &deletionRequests{}
	options := Options[*auditEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: DeleteSoft, EnableRestore: true, EnableDeletedQuery: true}
	app, err := NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionDelete, ActionPurge, ActionRestore, ActionListDeleted, "audit_read"} {
		t.Run(action, func(t *testing.T) {
			called := false
			callback := func(context.Context, *auditEntity) error { called = true; return nil }
			fresh := func(context.Context) (*auditEntity, error) { called = true; return &auditEntity{}, nil }
			bulkFresh := func(context.Context) ([]*auditEntity, error) { called = true; return nil, nil }
			_, one := app.Execute(t.Context(), action, 1, callback)
			_, newOne := app.ExecuteNew(t.Context(), action, fresh)
			_, bulk := app.ExecuteBulk(t.Context(), action, []int64{1}, callback)
			_, newBulk := app.ExecuteNewBulk(t.Context(), action, bulkFresh)
			_, empty := app.ExecuteBulk(t.Context(), action, nil, callback)
			for _, err := range []error{one, newOne, bulk, newBulk, empty} {
				if !errors.Is(err, errors.Forbidden) {
					t.Fatalf("lifecycle command result: %v", err)
				}
			}
			if called || len(policy.requests) != 0 {
				t.Fatalf("rejected action reached callback/authorizer: %t %+v", called, policy.requests)
			}
		})
	}
	for _, action := range []string{"delete", "restore", "purge", "list_deleted", "audit_read", " restore ", " delete ", " purge ", " list_deleted ", " audit_read "} {
		opts := options
		opts.EnableRestore = false
		opts.EnableDeletedQuery = false
		opts.Actions = []auth.Action{{Name: action, Kind: auth.KindUpdate}}
		if _, err := NewApplication(repo, opts); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("custom lifecycle %q accepted: %v", action, err)
		}
		if _, err := Describe(opts, true); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("Describe custom lifecycle %q: %v", action, err)
		}
	}
	for _, action := range []string{"delete", "restore", "list_deleted", " delete ", " restore ", " list_deleted "} {
		opts := Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeletePurge, Hooks: Hooks[*auditEntity]{action: {Before: func(context.Context, *auditEntity) error { return nil }}}}
		if _, err := NewApplication(repo, opts); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("disabled hook %q accepted: %v", action, err)
		}
	}
}

type deletionGuardEntity struct {
	domcrud.TrackedEntity[int64]
	Tenant string `dim:"tenant"`
	Owner  string `dim:"owner"`
	Name   string
}

func TestLifecycleBeforeCannotRedirectTargetOrVersion(t *testing.T) {
	for _, mode := range []DeleteMode{DeleteSoft, DeletePurge} {
		for _, attack := range []string{"id", "tenant", "owner", "version", "second-in-bulk"} {
			t.Run(string(rune(mode+'0'))+"/"+attack, func(t *testing.T) {
				repo, err := testkit.NewMemoryRepository[*deletionGuardEntity, int64](nil)
				if err != nil {
					t.Fatal(err)
				}
				first := &deletionGuardEntity{TrackedEntity: domcrud.TrackedEntity[int64]{Entity: domcrud.Entity[int64]{ID: 1}}, Tenant: "tenant-a", Owner: "owner-a", Name: "first"}
				second := &deletionGuardEntity{TrackedEntity: domcrud.TrackedEntity[int64]{Entity: domcrud.Entity[int64]{ID: 2}}, Tenant: "tenant-a", Owner: "owner-a", Name: "second"}
				for _, entity := range []*deletionGuardEntity{first, second} {
					if err := repo.Create(t.Context(), entity, spec.All()); err != nil {
						t.Fatal(err)
					}
				}
				action := ActionPurge
				if mode == DeleteSoft {
					action = ActionDelete
				}
				after := false
				opts := Options[*deletionGuardEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: mode, Hooks: Hooks[*deletionGuardEntity]{action: {
					Before: func(_ context.Context, entity *deletionGuardEntity) error {
						switch attack {
						case "id":
							entity.ID = 2
						case "tenant":
							entity.Tenant = "tenant-b"
						case "owner":
							entity.Owner = "owner-b"
						case "version":
							entity.Version++
						case "second-in-bulk":
							if entity.ID == 2 {
								entity.Version++
							}
						}
						return nil
					}, After: func(context.Context, *deletionGuardEntity) error { after = true; return nil },
				}}}
				app, err := NewApplication(repo, opts)
				if err != nil {
					t.Fatal(err)
				}
				if attack == "second-in-bulk" {
					err = app.DeleteBulk(t.Context(), []int64{1, 2})
				} else {
					err = app.Delete(t.Context(), 1)
				}
				if !errors.Is(err, errors.Forbidden) {
					t.Fatalf("hook mutation accepted: %v", err)
				}
				if attack != "second-in-bulk" && after {
					t.Fatal("rejected hook reached After")
				}
				for _, entity := range []*deletionGuardEntity{first, second} {
					loaded, err := repo.GetWithDeleted(t.Context(), entity.ID, spec.All())
					if err != nil || loaded.IsDeleted() || loaded.Version != entity.Version || loaded.Tenant != entity.Tenant || loaded.Owner != entity.Owner {
						t.Fatalf("hook changed storage: %+v %v", loaded, err)
					}
				}
			})
		}
	}
}

func TestHardDeleteBindingMatchesCompiledPurgeAction(t *testing.T) {
	repo := newTestRepo()
	options := Options[*testEntity, int64]{Resource: "document", DeleteMode: DeletePurge}
	descriptor, err := Describe(options, true)
	if err != nil {
		t.Fatal(err)
	}
	var checked []string
	policy, err := auth.Compile(auth.Declaration{Descriptor: descriptor, ResourceAccess: auth.ResourceAccessData}, auth.CompileOptions{
		Metadata: repo.ExecutionDescriptor().Metadata(),
		Checker:  auth.ActionCheckerFunc(func(_ context.Context, permission string) error { checked = append(checked, permission); return nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	options.Authorizer = policy
	app, err := NewApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	logical, err := app.AuthorizationBinding(ActionDelete)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := app.AuthorizationBinding(ActionPurge)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(logical, actual) {
		t.Fatalf("logical/actual binding drift: %+v %+v", logical, actual)
	}
	if err := app.FastDeny(t.Context(), ActionDelete); err != nil {
		t.Fatal(err)
	}
	for _, permission := range checked {
		if permission != "document:api:purge" {
			t.Fatalf("FastDeny used wrong permission: %v", checked)
		}
	}
}

func TestReadOnlyApplicationRejectsLifecycleConfiguration(t *testing.T) {
	repo := newDeletionRepo(t)
	for _, options := range []Options[*auditEntity, int64]{{DeleteMode: DeleteSoft}, {DeleteMode: DeletePurge}, {EnableRestore: true}, {EnableDeletedQuery: true}, {Actions: []auth.Action{{Name: "restore", Kind: auth.KindRead}}}} {
		options.Resource = "document"
		options.Authorizer = auth.Allow()
		if _, err := NewReadApplication(repo, options); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("read-only lifecycle accepted: %+v %v", options, err)
		}
		if _, err := Describe(options, false); !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("read-only Describe lifecycle accepted: %v", err)
		}
	}
	options := Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow()}
	app, err := NewReadApplication(repo, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Restore", "Purge", "ListDeleted", "DeleteAction"} {
		if _, ok := reflect.TypeOf(app).MethodByName(name); ok {
			t.Fatalf("read-only exposes %s", name)
		}
	}
}

func TestOrdinaryPurgeRequiresNoFabricatedIncludingDeletedRead(t *testing.T) {
	repo := newTestRepo()
	ports := purgePorts[*testEntity]{repo, repo}
	app, err := NewApplication(ports, Options[*testEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeletePurge})
	if err != nil {
		t.Fatal(err)
	}
	entity := &testEntity{Name: "ordinary"}
	if err := app.Create(t.Context(), entity); err != nil {
		t.Fatal(err)
	}
	if err := app.Delete(t.Context(), entity.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(t.Context(), entity.ID, spec.All()); !errors.Is(err, errors.NotFound) {
		t.Fatalf("ordinary purge: %v", err)
	}
}

func TestHardDeleteAuditFailureRollsBackAndPostCommitFailureStaysCommitted(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "bulk"}[bulk], func(t *testing.T) {
			repo := newDeletionRepo(t)
			ctx := newOperatorContext(t)
			first, second := &auditEntity{Name: "first"}, &auditEntity{Name: "second"}
			for _, entity := range []*auditEntity{first, second} {
				if err := repo.Create(ctx, entity, spec.All()); err != nil {
					t.Fatal(err)
				}
			}
			failing := hardDeletionAuditFailure{repo}
			callbacks := 0
			options := Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeletePurge, Audit: failing, Hooks: Hooks[*auditEntity]{ActionPurge: {PostCommit: func(context.Context, *auditEntity) error { callbacks++; return errors.New("postcommit failed") }}}}
			app, err := NewApplication(repo, options)
			if err != nil {
				t.Fatal(err)
			}
			run := func() error {
				if bulk {
					return app.DeleteBulk(ctx, []int64{first.ID, second.ID})
				}
				return app.Delete(ctx, first.ID)
			}
			if err := run(); err == nil || contextx.IsAfterCommitError(err) {
				t.Fatalf("expected rollback: %v", err)
			}
			if callbacks != 0 {
				t.Fatal("rolled back purge ran postcommit")
			}
			for _, entity := range []*auditEntity{first, second} {
				if _, err := repo.Get(ctx, entity.ID, spec.All()); err != nil {
					t.Fatalf("audit failure removed record: %v", err)
				}
			}
			options.Audit = repo
			app, err = NewApplication(repo, options)
			if err != nil {
				t.Fatal(err)
			}
			if err := run(); !contextx.IsAfterCommitError(err) {
				t.Fatalf("lost committed failure: %v", err)
			}
			want := 1
			if bulk {
				want = 2
			}
			if callbacks != want {
				t.Fatalf("callbacks=%d want %d", callbacks, want)
			}
			if _, err := repo.GetWithDeleted(ctx, first.ID, spec.All()); !errors.Is(err, errors.NotFound) {
				t.Fatalf("postcommit purge rolled back: %v", err)
			}
		})
	}
}

type hardDeletionAuditFailure struct {
	*testkit.AuditedMemoryRepository[*auditEntity, int64]
}

func (hardDeletionAuditFailure) SaveAuditRecord(context.Context, domaudited.AuditRecord) (int64, error) {
	return 0, errors.New("audit failed")
}
func (hardDeletionAuditFailure) SaveAuditRecords(context.Context, []domaudited.AuditRecord) error {
	return errors.New("audit failed")
}
