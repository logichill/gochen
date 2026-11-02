package testkit_test

import (
	"gochen/domain/meta"
	"gochen/testkit"
	"gochen/testkit/contracttest"
	"testing"
)

func TestMemoryDimensionsContract(t *testing.T) {
	contracttest.RunDimensions(t, func(t *testing.T) contracttest.Repository {
		r, err := testkit.NewAuditedMemoryRepository[*contracttest.Document, int64](testkit.NewInt64Sequence(1),
			testkit.WithMemoryResourceContract[*contracttest.Document, int64](contracttest.Contract()),
			testkit.WithMemoryIsolationField[*contracttest.Document, int64](meta.ByDimension("tenant"), nil))
		if err != nil {
			t.Fatal(err)
		}
		return r
	})
}
