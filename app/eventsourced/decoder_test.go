package eventsourced

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gochen/errors"
	"gochen/eventing"
)

// TestDefaultPayloadDecoder 验证 DefaultPayloadDecoder。
func TestDefaultPayloadDecoder(t *testing.T) {
	t.Run("nil event", func(t *testing.T) {
		dec := defaultPayloadDecoder[int]()
		_, err := dec(nil)
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.InvalidInput))
	})

	t.Run("payload type mismatch", func(t *testing.T) {
		dec := defaultPayloadDecoder[int]()
		evt := eventing.NewEvent[int64](1, "Agg", "TypeA", 1, "not-an-int")
		_, err := dec(evt)
		require.Error(t, err)
		require.True(t, errors.Is(err, errors.InvalidInput))
	})

	t.Run("ok", func(t *testing.T) {
		dec := defaultPayloadDecoder[int]()
		evt := eventing.NewEvent[int64](1, "Agg", "TypeA", 1, 42)
		got, err := dec(evt)
		require.NoError(t, err)
		require.Equal(t, 42, got)
	})
}
