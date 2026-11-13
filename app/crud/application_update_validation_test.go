package crud

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"gochen/app"
	"gochen/auth"
	domcrud "gochen/domain/crud"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit"
)

type updateValidationEntity struct {
	domcrud.TrackedEntity[int64]
	Tenant     string  `dim:"tenant"`
	Owner      *string `dim:"owner"`
	Classified bool    `dim:"classification"`
	Name       string
}

func TestOrdinaryUpdateChecksLoadedTargetAfterEveryMutableStage(t *testing.T) {
	for _, entry := range []string{"update", "execute", "bulk"} {
		for _, phase := range []string{"callback", "before", "validator"} {
			if entry == "update" && phase == "callback" {
				continue
			}
			for _, attack := range []string{"id", "version", "tenant", "owner-pointer"} {
				t.Run(entry+"/"+phase+"/"+attack, func(t *testing.T) {
					repo, err := testkit.NewAuditedMemoryRepository[*updateValidationEntity, int64](nil)
					if err != nil {
						t.Fatal(err)
					}
					originals := []*updateValidationEntity{}
					for _, id := range []int64{1, 2} {
						owner := "owner-a"
						entity := &updateValidationEntity{TrackedEntity: domcrud.TrackedEntity[int64]{Entity: domcrud.Entity[int64]{ID: id}}, Tenant: "tenant-a", Owner: &owner, Name: "original"}
						if err := repo.Create(t.Context(), entity, spec.All()); err != nil {
							t.Fatal(err)
						}
						stored, err := repo.Get(t.Context(), id, spec.All())
						if err != nil {
							t.Fatal(err)
						}
						originals = append(originals, stored)
					}
					policy := &deletionRequests{allow: ActionUpdate}
					mutate := func(entity *updateValidationEntity) {
						if entry == "bulk" && entity.ID == 1 {
							return
						}
						switch attack {
						case "id":
							if entity.ID == 1 {
								entity.ID = 2
							} else {
								entity.ID = 1
							}
						case "version":
							entity.Version++
						case "tenant":
							entity.Tenant = "tenant-b"
						case "owner-pointer":
							*entity.Owner = "owner-b"
						}
					}
					opts := Options[*updateValidationEntity, int64]{Resource: "document", Authorizer: policy, DeleteMode: DeletePurge, Audit: repo}
					if phase == "before" {
						opts.Hooks = Hooks[*updateValidationEntity]{ActionUpdate: {Before: func(_ context.Context, entity *updateValidationEntity) error { mutate(entity); return nil }}}
					}
					if phase == "validator" {
						opts.Validator = restoreValidationFunc(func(value any) error { mutate(value.(*updateValidationEntity)); return nil })
					}
					application, err := NewApplication(repo, opts)
					if err != nil {
						t.Fatal(err)
					}
					callback := func(_ context.Context, entity *updateValidationEntity) error {
						entity.Name = "pending change"
						if phase == "callback" {
							mutate(entity)
						}
						return nil
					}
					switch entry {
					case "update":
						entity, readErr := repo.Get(t.Context(), 1, spec.All())
						if readErr != nil {
							t.Fatal(readErr)
						}
						entity.Name = "pending change"
						err = application.Update(newOperatorContext(t), entity)
					case "execute":
						_, err = application.Execute(newOperatorContext(t), ActionUpdate, 1, callback)
					case "bulk":
						_, err = application.ExecuteBulk(newOperatorContext(t), ActionUpdate, []int64{1, 2}, callback)
					}
					if !errors.Is(err, errors.Forbidden) {
						t.Fatalf("mutable stage changed target: %v", err)
					}
					if len(policy.requests) != 1 || policy.requests[0].Action != ActionUpdate {
						t.Fatalf("wrong authorization: %+v", policy.requests)
					}
					for _, original := range originals {
						stored, err := repo.Get(t.Context(), original.ID, spec.All())
						if err != nil || !reflect.DeepEqual(stored, original) {
							t.Fatalf("mutation committed/partial bulk: %+v want %+v err=%v", stored, original, err)
						}
						records, err := repo.ListAuditRecords(t.Context(), strconv.FormatInt(original.ID, 10), 0, 10, spec.All())
						if err != nil || len(records) != 0 {
							t.Fatalf("rejected update wrote audit: %+v %v", records, err)
						}
					}
				})
			}
		}
	}
}

func TestUpdateValidatorPreservesExplicitMutableDimensions(t *testing.T) {
	repo, err := testkit.NewMemoryRepository[*updateValidationEntity, int64](nil, testkit.WithMemoryResourceContract[*updateValidationEntity, int64](app.ResourceContract{Mutable: []meta.Dimension{"classification"}}))
	if err != nil {
		t.Fatal(err)
	}
	owner := "owner-a"
	entity := &updateValidationEntity{TrackedEntity: domcrud.TrackedEntity[int64]{Entity: domcrud.Entity[int64]{ID: 1}}, Tenant: "tenant-a", Owner: &owner, Name: "original"}
	if err := repo.Create(t.Context(), entity, spec.All()); err != nil {
		t.Fatal(err)
	}
	application, err := NewApplication(repo, Options[*updateValidationEntity, int64]{Resource: "document", Authorizer: auth.Allow(), DeleteMode: DeletePurge, Validator: restoreValidationFunc(func(value any) error { entity := value.(*updateValidationEntity); entity.Classified = true; return nil })})
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.Execute(t.Context(), ActionUpdate, 1, func(_ context.Context, entity *updateValidationEntity) error { entity.Name = "changed"; return nil })
	if err != nil || result == nil || !result.Classified || result.Name != "changed" || result.Version != 1 {
		t.Fatalf("legal mutable update changed: %+v %v", result, err)
	}
}
