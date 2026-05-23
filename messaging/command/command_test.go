package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gochen/contextx"
	"gochen/messaging"
)

// TestNewCommand 验证 NewCommand。
func TestNewCommand(t *testing.T) {
	payload := map[string]any{"name": "test"}
	cmd := NewCommand("cmd-1", "CreateUser", "123", "User", payload)

	assert.Equal(t, "cmd-1", cmd.GetID())
	assert.Equal(t, messaging.KindCommand, cmd.GetKind())
	assert.Equal(t, "CreateUser", cmd.GetType())
	assert.Equal(t, "123", cmd.GetAggregateID())
	assert.Equal(t, "User", cmd.GetAggregateType())
	assert.Equal(t, payload, messaging.PayloadValue(cmd.GetPayload()))
	assert.NotNil(t, cmd.GetTimestamp())
	assert.NotNil(t, cmd.GetMetadata())
}

// TestCommand_GetCommandType 验证 Command GetCommandType。
func TestCommand_GetCommandType(t *testing.T) {
	cmd := NewCommand("cmd-1", "CreateUser", "123", "User", nil)
	assert.Equal(t, "CreateUser", cmd.GetCommandType())
}

// TestCommand_ChainedMethods 验证 Command ChainedMethods。
func TestCommand_ChainedMethods(t *testing.T) {
	cmd := NewCommand("cmd-1", "CreateUser", "123", "User", nil).
		WithMetadata("user_id", "456").
		WithMetadata("request_id", "req-1").
		WithMetadata(contextx.MetadataTraceKey, "trc-1").
		WithMetadata("causation_id", "cause-1").
		WithMetadata("custom", "value")

	vUserID, ok := cmd.GetMetadata().GetString("user_id")
	assert.True(t, ok)
	assert.Equal(t, "456", vUserID)

	vReqID, ok := cmd.GetMetadata().GetString("request_id")
	assert.True(t, ok)
	assert.Equal(t, "req-1", vReqID)

	vTraceID, ok := cmd.GetMetadata().GetString(contextx.MetadataTraceKey)
	assert.True(t, ok)
	assert.Equal(t, "trc-1", vTraceID)

	vCausID, ok := cmd.GetMetadata().GetString("causation_id")
	assert.True(t, ok)
	assert.Equal(t, "cause-1", vCausID)

	vCustom, ok := cmd.GetMetadata().GetString("custom")
	assert.True(t, ok)
	assert.Equal(t, "value", vCustom)
}
