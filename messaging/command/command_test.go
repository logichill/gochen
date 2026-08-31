package command

import (
	"testing"
	"time"

	"gochen/testkit/assert"
	"gochen/testkit/require"

	"gochen/clock"
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

func TestNewCommandWithClockUsesInjectedTime(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	cmd := NewCommandWithClock(clock.NewManualClock(now), "cmd-1", "CreateUser", "123", "User", nil)
	require.Equal(t, now, cmd.GetTimestamp())
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

func TestCommand_CloneMessageEnvelopeClonesNilMapSliceElements(t *testing.T) {
	items := []map[string]any{nil, {"name": "original"}}
	cmd := NewCommand("cmd-1", "CreateUser", "123", "User", map[string]any{"items": items})
	cmd.WithMetadata("trace_id", "trc-1")

	cloned := cmd.CloneMessageEnvelope().(*Command)
	items[1]["name"] = "mutated"
	cmd.Type = "mutated"
	cmd.GetMetadata().Set("trace_id", "mutated")

	payload := messaging.PayloadValue(cloned.GetPayload()).(map[string]any)
	clonedItems := payload["items"].([]map[string]any)
	assert.Nil(t, clonedItems[0])
	assert.Equal(t, "original", clonedItems[1]["name"])
	assert.Equal(t, "CreateUser", cloned.GetType())
	traceID, ok := cloned.GetMetadata().GetString("trace_id")
	assert.True(t, ok)
	assert.Equal(t, "trc-1", traceID)
}
