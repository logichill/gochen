package saga

import (
	"errors"
	"testing"
	"time"

	"gochen/testkit/require"

	"gochen/clock"
	"gochen/messaging/command"
)

func TestSagaStateMarkFailedRecordsErrorAndTime(t *testing.T) {
	start := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	manualClock := clock.NewManualClock(start)
	state := newSagaStateWithClock("saga-1", "test", manualClock)

	manualClock.Advance(time.Minute)
	state.MarkFailed(errors.New("boom"))

	require.Equal(t, SagaStatusFailed, state.Status)
	require.Equal(t, "boom", state.Error)
	require.Equal(t, manualClock.Now(), state.UpdatedAt)
}

func TestSagaStateWithClockControlsTransitionsAndClone(t *testing.T) {
	start := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	manualClock := clock.NewManualClock(start)
	state := NewSagaState("saga-1", "test").WithClock(manualClock)

	manualClock.Advance(time.Hour)
	state.MarkRunning()
	state.SetData("key", "value")

	require.Equal(t, manualClock.Now(), state.UpdatedAt)
	clone := state.Clone()
	manualClock.Advance(time.Minute)
	clone.MarkCompleted()
	require.Equal(t, manualClock.Now(), clone.UpdatedAt)
}

func TestSagaStateWithClockIgnoresTypedNil(t *testing.T) {
	state := NewSagaState("saga-1", "test")
	originalClock := state.clock
	var typedNil *clock.ManualClock

	state.WithClock(typedNil)
	require.Same(t, originalClock, state.clock)
}

func TestSagaIdempotencyKeysEncodeEdgeCaseStepNames(t *testing.T) {
	cases := []struct {
		name     string
		stepName string
	}{
		{name: "empty", stepName: ""},
		{name: "unicode", stepName: "审批/确认"},
		{name: "special", stepName: "step name/?#=&+"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stepKey := sagaStepIdempotencyKey("saga-1", tc.stepName)
			compKey := sagaCompensationIdempotencyKey("saga-1", tc.stepName)

			require.NotEmpty(t, stepKey)
			require.NotEmpty(t, compKey)
			require.NotContains(t, stepKey, " ")
			require.NotContains(t, compKey, " ")
		})
	}
}

func TestEnsureCommandIdempotencyKeyPreservesExistingKey(t *testing.T) {
	cmd := command.NewCommand("cmd-1", "TestCommand", "1", "Test", nil)
	cmd.WithMetadata(command.MetadataIdempotencyKey, "caller-key")

	ensureCommandIdempotencyKey(cmd, "saga-key")

	got, ok := cmd.GetMetadata().GetString(command.MetadataIdempotencyKey)
	require.True(t, ok)
	require.Equal(t, "caller-key", got)
}
