package monitoring

import (
	"testing"

	"gochen/testkit/require"
)

// TestSetDefaultRegistry_ReturnsErrorOnNil 验证 SetDefaultRegistry ReturnsErrorOnNil。
func TestSetDefaultRegistry_ReturnsErrorOnNil(t *testing.T) {
	require.Error(t, SetDefaultRegistry(nil))
}
