package testkit_test

import (
	"testing"

	"gochen/app"
	appcrud "gochen/app/crud"
	"gochen/auth"
	"gochen/domain/meta"
	"gochen/errors"
	"gochen/testkit"
)

type unversionedTransferEntity struct {
	ID         int64 `entity:"id"`
	Department int64 `dim:"department"`
}

func (e *unversionedTransferEntity) GetID() int64       { return e.ID }
func (e *unversionedTransferEntity) GetVersion() uint64 { return 0 }

func TestTransferContractRequiresVersionSetter(t *testing.T) {
	_, err := testkit.NewMemoryRepository[*unversionedTransferEntity, int64](nil,
		testkit.WithMemoryResourceContract[*unversionedTransferEntity, int64](app.ResourceContract{Transferable: []meta.Dimension{"department"}}))
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("unversioned transfer declaration: %v", err)
	}
}

func TestTransferApplicationRequiresVersionSetter(t *testing.T) {
	r, err := testkit.NewMemoryRepository[*unversionedTransferEntity, int64](nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = appcrud.NewApplication(r, appcrud.Options[*unversionedTransferEntity, int64]{DeleteMode: appcrud.DeletePurge, Resource: "document", Authorizer: auth.Allow(),
		Actions: []auth.Action{{Name: "receive", Kind: auth.KindTransfer}}})
	if !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("unversioned transfer application: %v", err)
	}
}
