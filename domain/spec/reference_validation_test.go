package spec_test

import (
	"gochen/domain/meta"
	"gochen/domain/spec"
	"testing"
)

func TestLogicalResultPreservesReferenceAndConversionErrors(t *testing.T) {
	entity := &struct{ Value int64 }{Value: 1}
	for _, condition := range []spec.Spec{
		spec.Or(spec.All(), spec.Eq(meta.ByField("Missing"), 1)),
		spec.And(spec.Spec{}, spec.Eq(meta.ByField("Missing"), 1)),
		spec.Or(spec.Eq(meta.ByField("Value"), 1), spec.Eq(meta.ByField("Value"), "invalid")),
		spec.In(meta.ByField("Value"), 1, "invalid"),
		spec.In(meta.ByField("Missing")),
	} {
		if _, err := spec.NewInterpreter().Match(condition, entity); err == nil {
			t.Fatalf("condition %s hid an invalid reference/value", condition.Canonical())
		}
		if len(condition.Refs()) == 0 {
			t.Fatal("logical folding discarded references")
		}
	}
}
