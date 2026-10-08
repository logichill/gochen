package saga

import (
	"context"
	stdErrors "errors"
	"testing"

	"gochen/testkit/require"

	"gochen/errors"

	"gochen/eventing/bus"
	"gochen/gen"

	"gochen/messaging/command"

	"gochen/observe/logging"
)

func TestNewSagaOrchestratorNormalizesOptionalTypedNilDependencies(t *testing.T) {
	var commandExecutor *command.CommandExecutor
	var eventBus *bus.EventBus
	var stateStore *MemorySagaStateStore

	orchestrator, err := NewSagaOrchestrator(
		commandExecutor,
		eventBus,
		stateStore,
		gen.NewUUIDGenerator(),
		logging.NewNoopLogger(),
	)
	require.NoError(t, err)
	require.Nil(t, orchestrator.commandExecutor)
	require.Nil(t, orchestrator.eventBus)
	require.Nil(t, orchestrator.stateStore)

	err = orchestrator.Execute(context.Background(), &simpleSaga{})
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestSagaOrchestrator_InvalidInput_FailFast(t *testing.T) {
	cmdExecutor := newTestCommandExecutor()
	orchestrator := newTestSagaOrchestrator(t, cmdExecutor, &mockSagaEventBus{}, NewMemorySagaStateStore())

	err := orchestrator.Execute(nil, &simpleSaga{})
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = orchestrator.Execute(context.Background(), nil)
	require.True(t, errors.Is(err, errors.InvalidInput))

	saga := &resumeSaga{id: "resume-invalid-input"}
	saga.steps = []*SagaStep{
		NewSagaStep("step1", func(ctx context.Context) (*command.Command, error) {
			return command.NewCommand("c1", "Cmd1", "1", "Step1", nil), nil
		}),
	}

	err = orchestrator.Resume(nil, saga, NewSagaState(saga.ID(), "resumeSaga"))
	require.True(t, errors.Is(err, errors.InvalidInput))

	err = orchestrator.Resume(context.Background(), nil, NewSagaState("resume-invalid-input", "resumeSaga"))
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestNewSagaOrchestrator_RejectsNilDependencies(t *testing.T) {
	cases := []struct {
		name   string
		g      gen.IGenerator[string]
		logger logging.ILogger
	}{
		{"nil event ID generator", nil, logging.NewNoopLogger()},
		{"typed-nil event ID generator", (*stubSagaStringGenerator)(nil), logging.NewNoopLogger()},
		{"nil logger", gen.NewUUIDGenerator(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSagaOrchestrator(nil, nil, nil, tc.g, tc.logger)
			require.Error(t, err)
			require.True(t, errors.Is(err, errors.InvalidInput))
		})
	}
}

// TestBaseSaga_DefaultCallbacks_NoError 验证 BaseSaga DefaultCallbacks NoError。
func TestBaseSaga_DefaultCallbacks_NoError(t *testing.T) {
	var b BaseSaga
	require.NoError(t, b.OnComplete(context.Background()))
	require.NoError(t, b.OnFailed(context.Background(), stdErrors.New("x")))
}
