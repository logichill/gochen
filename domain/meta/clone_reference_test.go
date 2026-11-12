package meta_test

import (
	"testing"
	"time"

	"gochen/domain/meta"
	"gochen/errors"
	"gochen/testkit/require"
)

func TestCloneRejectsPrivateMutableReferences(t *testing.T) {
	for _, input := range []any{
		struct{ state map[string]string }{map[string]string{"name": "before"}},
		struct{ state []string }{[]string{"before"}},
		struct{ state *string }{new(string)},
		struct{ state any }{map[string]string{}},
		struct{ state chan int }{make(chan int)},
		struct{ state func() }{func() {}},
	} {
		_, err := meta.Clone(input)
		require.True(t, errors.Is(err, errors.Unsupported))
	}
}

func TestClonePreservesImmutablePrivateValues(t *testing.T) {
	now := time.Now()
	input := struct {
		name  string
		times [2]time.Time
	}{"before", [2]time.Time{now, now.In(time.FixedZone("test", 3600))}}
	copy, err := meta.Clone(input)
	require.NoError(t, err)
	require.Equal(t, input, copy)
}
