package crud

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gochen/auth"
	"gochen/domain"
	domcrud "gochen/domain/crud"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

func TestOrdinaryUpdatesCannotChangeDeletionState(t *testing.T) {
	for _, mode := range []DeleteMode{DeleteSoft, DeletePurge} {
		for _, path := range []string{"update-input", "update-before", "execute-callback", "execute-before", "bulk-callback", "bulk-before"} {
			for _, mutation := range []string{"soft-delete", "deleted-by", "deleted-by-pointer"} {
				t.Run(map[DeleteMode]string{DeleteSoft: "soft", DeletePurge: "purge"}[mode]+"/"+path+"/"+mutation, func(t *testing.T) {
					repo := newDeletionRepo(t)
					ctx := newOperatorContext(t)
					first, second := &auditEntity{Name: "first"}, &auditEntity{Name: "second"}
					if mutation == "deleted-by-pointer" {
						by := "legacy"
						first.DeletedBy = &by
						other := "legacy"
						second.DeletedBy = &other
					}
					for _, entity := range []*auditEntity{first, second} {
						if err := repo.Create(ctx, entity, spec.All()); err != nil {
							t.Fatal(err)
						}
					}
					originalFirst, err := repo.Get(ctx, first.ID, spec.All())
					if err != nil {
						t.Fatal(err)
					}
					originalSecond, err := repo.Get(ctx, second.ID, spec.All())
					if err != nil {
						t.Fatal(err)
					}
					policy := &deletionRequests{allow: ActionUpdate}
					mutate := func(entity *auditEntity) error {
						entity.Name = "unauthorized business change"
						switch mutation {
						case "soft-delete":
							return entity.SoftDeleteBy("intruder", time.Now())
						case "deleted-by":
							by := "intruder"
							entity.DeletedBy = &by
						case "deleted-by-pointer":
							*entity.DeletedBy = "intruder"
						}
						return nil
					}
					options := Options[*auditEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: mode, Audit: repo}
					bulk := path == "bulk-callback" || path == "bulk-before"
					if path == "update-before" || path == "execute-before" || path == "bulk-before" {
						options.Hooks = Hooks[*auditEntity]{ActionUpdate: {Before: func(_ context.Context, entity *auditEntity) error {
							if !bulk || entity.ID == second.ID {
								return mutate(entity)
							}
							entity.Name = "first pending change"
							return nil
						}}}
					}
					app, err := NewApplication(repo, options)
					if err != nil {
						t.Fatal(err)
					}
					callback := func(_ context.Context, entity *auditEntity) error {
						if path == "execute-callback" || (path == "bulk-callback" && entity.ID == second.ID) {
							return mutate(entity)
						}
						entity.Name = "first pending change"
						return nil
					}
					switch path {
					case "update-input":
						entity, loadErr := repo.Get(ctx, first.ID, spec.All())
						if loadErr != nil {
							t.Fatal(loadErr)
						}
						if err := mutate(entity); err != nil {
							t.Fatal(err)
						}
						err = app.Update(ctx, entity)
					case "update-before":
						entity, loadErr := repo.Get(ctx, first.ID, spec.All())
						if loadErr != nil {
							t.Fatal(loadErr)
						}
						entity.Name = "business change"
						err = app.Update(ctx, entity)
					case "execute-callback", "execute-before":
						_, err = app.Execute(ctx, ActionUpdate, first.ID, callback)
					default:
						_, err = app.ExecuteBulk(ctx, ActionUpdate, []int64{first.ID, second.ID}, callback)
					}
					if !errors.Is(err, errors.Forbidden) {
						t.Fatalf("deletion change accepted: %v", err)
					}
					if len(policy.requests) != 1 || policy.requests[0].Action != ActionUpdate {
						t.Fatalf("wrong authorization path: %+v", policy.requests)
					}
					for _, original := range []*auditEntity{originalFirst, originalSecond} {
						stored, err := repo.GetWithDeleted(ctx, original.ID, spec.All())
						if err != nil || !reflect.DeepEqual(stored, original) {
							t.Fatalf("rejected deletion changed storage: %+v want %+v err=%v", stored, original, err)
						}
					}
					records, err := repo.ListAuditRecords(ctx, "1", 0, 10, spec.All())
					if err != nil || len(records) != 0 {
						t.Fatalf("rejected deletion wrote audit: %+v %v", records, err)
					}
				})
			}
		}
	}
}

func TestOrdinaryCreationRejectsDeletedStateAndRollsBackBulk(t *testing.T) {
	for _, mode := range []DeleteMode{DeleteSoft, DeletePurge} {
		for _, path := range []string{"create-input", "create-before", "new-result", "new-before", "bulk-result", "bulk-before"} {
			for _, mutation := range []string{"soft-delete", "deleted-by"} {
				t.Run(map[DeleteMode]string{DeleteSoft: "soft", DeletePurge: "purge"}[mode]+"/"+path+"/"+mutation, func(t *testing.T) {
					repo := newDeletionRepo(t)
					ctx := newOperatorContext(t)
					policy := &deletionRequests{allow: ActionCreate}
					mutate := func(entity *auditEntity) error {
						if mutation == "soft-delete" {
							return entity.SoftDelete(time.Now())
						}
						by := "intruder"
						entity.DeletedBy = &by
						return nil
					}
					bulk := path == "bulk-result" || path == "bulk-before"
					options := Options[*auditEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: mode, Audit: repo}
					if path == "create-before" || path == "new-before" || path == "bulk-before" {
						options.Hooks = Hooks[*auditEntity]{ActionCreate: {Before: func(_ context.Context, entity *auditEntity) error {
							if !bulk || entity.Name == "second" {
								return mutate(entity)
							}
							return nil
						}}}
					}
					app, err := NewApplication(repo, options)
					if err != nil {
						t.Fatal(err)
					}
					first, second := &auditEntity{Name: "first"}, &auditEntity{Name: "second"}
					switch path {
					case "create-input":
						if err := mutate(first); err != nil {
							t.Fatal(err)
						}
						err = app.Create(ctx, first)
					case "create-before":
						err = app.Create(ctx, first)
					case "new-result":
						_, err = app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*auditEntity, error) {
							if err := mutate(first); err != nil {
								return nil, err
							}
							return first, nil
						})
					case "new-before":
						_, err = app.ExecuteNew(ctx, ActionCreate, func(context.Context) (*auditEntity, error) { return first, nil })
					case "bulk-result":
						_, err = app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*auditEntity, error) {
							if err := mutate(second); err != nil {
								return nil, err
							}
							return []*auditEntity{first, second}, nil
						})
					case "bulk-before":
						_, err = app.ExecuteNewBulk(ctx, ActionCreate, func(context.Context) ([]*auditEntity, error) { return []*auditEntity{first, second}, nil })
					}
					if !errors.Is(err, errors.Forbidden) {
						t.Fatalf("deleted creation accepted: %v", err)
					}
					if len(policy.requests) != 1 || policy.requests[0].Action != ActionCreate {
						t.Fatalf("wrong create authorization: %+v", policy.requests)
					}
					if count, err := repo.Count(ctx, spec.All()); err != nil || count != 0 {
						t.Fatalf("rejected creation committed active row count=%d err=%v", count, err)
					}
					if deleted, err := repo.ListDeleted(ctx, 0, 10, spec.All()); err != nil || len(deleted) != 0 {
						t.Fatalf("rejected creation committed deleted row: %+v %v", deleted, err)
					}
					records, err := repo.ListAuditRecords(ctx, "1", 0, 10, spec.All())
					if err != nil || len(records) != 0 {
						t.Fatalf("rejected creation wrote audit: %+v %v", records, err)
					}
				})
			}
		}
	}
}

// A separate flag proves that the guard checks the state method as well as the timestamp.
type flaggedDeletionEntity struct {
	domcrud.Entity[int64]
	domain.DeletionState
	DeletedFlag bool `managed:"true"`
	Name        string
}

func (e *flaggedDeletionEntity) IsDeleted() bool { return e.DeletedFlag }

func TestOrdinaryWritesProtectIndependentDeletedFlag(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*flaggedDeletionEntity, int64](testkit.NewInt64Sequence(1))
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApplication(repo, Options[*flaggedDeletionEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeleteSoft})
	if err != nil {
		t.Fatal(err)
	}
	entity := &flaggedDeletionEntity{Name: "first"}
	if err := app.Create(t.Context(), entity); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Execute(t.Context(), ActionUpdate, entity.ID, func(_ context.Context, e *flaggedDeletionEntity) error {
		e.Name = "changed"
		e.DeletedFlag = true
		return nil
	}); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("independent deletion flag bypassed: %v", err)
	}
	if err := app.Create(t.Context(), &flaggedDeletionEntity{DeletedFlag: true, Name: "born deleted"}); !errors.Is(err, errors.Forbidden) {
		t.Fatalf("independent initial flag bypassed: %v", err)
	}
}

func TestOrdinaryWritesDoNotFreezeUnrelatedManagedAttribution(t *testing.T) {
	repo := newDeletionRepo(t)
	app, err := NewApplication(repo, Options[*auditEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeleteSoft, EnableRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	entity := &auditEntity{Name: "first"}
	if err := app.Create(t.Context(), entity); err != nil {
		t.Fatal(err)
	}
	result, err := app.Execute(t.Context(), ActionUpdate, entity.ID, func(_ context.Context, e *auditEntity) error {
		e.Name = "changed"
		e.SetUpdatedInfo("custom actor", time.Now())
		return nil
	})
	if err != nil || result.Name != "changed" {
		t.Fatalf("legitimate managed attribution rejected: %+v %v", result, err)
	}
	if err := app.Delete(t.Context(), entity.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Restore(t.Context(), entity.ID); err != nil {
		t.Fatalf("lifecycle restore rejected by ordinary guard: %v", err)
	}
	if err := app.Purge(t.Context(), entity.ID); err != nil {
		t.Fatal(err)
	}
}
