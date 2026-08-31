package store

import (
	"encoding/json"
	"testing"

	"gochen/testkit/require"
)

func TestStreamResultJSONOmitsEmptyOptionalFields(t *testing.T) {
	data, err := json.Marshal(StreamResult[int64]{})
	require.NoError(t, err)
	require.JSONEq(t, `{"events":null}`, string(data))
}

func TestStreamResultJSONIncludesCursorFields(t *testing.T) {
	data, err := json.Marshal(StreamResult[int64]{
		NextCursor:   "cursor-1",
		EventCursors: []string{"cursor-1"},
		HasMore:      true,
	})
	require.NoError(t, err)
	require.Contains(t, string(data), `"next_cursor":"cursor-1"`)
	require.Contains(t, string(data), `"event_cursors":["cursor-1"]`)
	require.Contains(t, string(data), `"has_more":true`)
}
