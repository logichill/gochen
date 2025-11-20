package process

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestManager_HandleCommand_WithMemoryStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	mgr := NewManager(store)
	id := ID("saga-1")

	err := mgr.HandleCommand(ctx, id, func(st *State) error {
		if st.Data == nil {
			st.Data = map[string]interface{}{}
		}
		st.Data["step"] = "debit_done"
		return nil
	})
	require.NoError(t, err)

	st, _ := store.Get(ctx, id)
	require.NotNil(t, st)
	require.Equal(t, "debit_done", st.Data["step"])
}
