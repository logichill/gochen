package auth_test

import (
	"gochen/auth"
	"gochen/domain/meta"
	"gochen/domain/spec"
	"gochen/errors"
	"gochen/testkit/require"
	"testing"
)

type vocabularyEntity struct {
	ID     int64  `entity:"id"`
	Owner  string `dim:"owner"`
	Scope  int64  `dim:"scope"`
	Status string
}

func TestVocabularyAndActualDecision(t *testing.T) {
	metadata, err := meta.Describe[*vocabularyEntity]()
	require.NoError(t, err)
	v := auth.Vocabulary{Refs: []meta.Ref{meta.ByDimension("owner"), meta.ByField("Status")}, Assignable: []meta.Dimension{"owner"}}
	require.NoError(t, v.Validate(metadata))
	for _, invalid := range []auth.Vocabulary{
		{Refs: []meta.Ref{meta.ByDimension("missing")}}, {Refs: []meta.Ref{meta.ByField("missing")}},
		{Refs: []meta.Ref{{}}}, {Assignable: []meta.Dimension{"missing"}},
	} {
		require.ErrorIs(t, invalid.Validate(metadata), errors.InvalidInput)
	}
	require.NoError(t, (auth.Decision{}).ValidateAgainst(v, metadata))
	valid := auth.Decision{Allowed: true, Spec: spec.And(spec.Eq(meta.ByField("Owner"), "alice"), spec.Eq(meta.ByField("Status"), "open")), Assign: []auth.Assignment{{Dimension: "owner", Value: "alice"}}}
	require.NoError(t, valid.ValidateAgainst(v, metadata))
	for _, invalid := range []auth.Decision{
		{Allowed: true, Spec: spec.Eq(meta.ByDimension("scope"), 1)},
		{Allowed: true, Spec: spec.Named("active")},
		{Allowed: true, Assign: []auth.Assignment{{Dimension: "scope", Value: 1}}},
		{Allowed: true, Assign: []auth.Assignment{{Dimension: "owner", Value: "a"}, {Dimension: "owner", Value: "b"}}},
	} {
		require.ErrorIs(t, invalid.ValidateAgainst(v, metadata), errors.Internal)
	}
}
