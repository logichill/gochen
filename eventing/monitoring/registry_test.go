package monitoring

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSetDefaultRegistry_ReturnsErrorOnNil 验证 SetDefaultRegistry ReturnsErrorOnNil。
func TestSetDefaultRegistry_ReturnsErrorOnNil(t *testing.T) {
	require.Error(t, SetDefaultRegistry(nil))
}
