package eventsourced

import (
	"context"
	"testing"

	"gochen/app/operation"
	"gochen/errors"
	"gochen/observe/logging"
)

type nilCommandLogger struct{ logging.ILogger }
type nilCommandTracer struct{ ICommandTracer }

func TestCommandServiceRejectsTypedNilRepository(t *testing.T) {
	var repository *okRepo
	service, err := NewEventSourcedService[*retryTestAggregate, int64](repository, nil)
	if service != nil || !errors.Is(err, errors.InvalidInput) {
		t.Fatalf("service=%v, err=%v", service, err)
	}
}

func TestCommandServiceNormalizesOptionalTypedNilDependencies(t *testing.T) {
	var logger *nilCommandLogger
	var tracer *nilCommandTracer
	var hook *recordingHook
	var runner *operation.Runner
	service, err := NewEventSourcedService[*retryTestAggregate, int64](&okRepo{}, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		Logger: logger, CommandTracer: tracer, OperationRunner: runner,
		CommandHooks: []IEventSourcedCommandHook[*retryTestAggregate, int64]{hook},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterCommandHandler((*retryTestCommand)(nil), func(context.Context, IEventSourcedCommand[int64], *retryTestAggregate) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := service.ExecuteCommand(context.Background(), &retryTestCommand{}); err != nil {
		t.Fatal(err)
	}
	_, err = service.ExecuteCommandWithOperation(context.Background(), &retryTestCommand{}, &operation.Spec{Type: "test", Mode: operation.ModeTracked})
	if !errors.Is(err, errors.FailedPrecondition) {
		t.Fatal(err)
	}
}

func TestCommandServiceRejectsTypedNilCommandsBeforeResolversAndRunner(t *testing.T) {
	service, err := NewEventSourcedService[*retryTestAggregate, int64](&okRepo{}, &EventSourcedServiceOptions[*retryTestAggregate, int64]{
		OperationSpecResolver: func(context.Context, IEventSourcedCommand[int64]) (*operation.Spec, error) {
			t.Fatal("resolver called with nil command")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var cmd *retryTestCommand
	if err := service.RegisterCommandHandler(cmd, func(context.Context, IEventSourcedCommand[int64], *retryTestAggregate) error {
		t.Fatal("nil command executed")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ExecuteCommand(context.Background(), cmd); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
	if _, err := service.ExecuteCommandWithResolvedOperation(context.Background(), cmd); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
	if _, err := service.ExecuteCommandWithOperation(context.Background(), cmd, &operation.Spec{Type: "test", Mode: operation.ModeTracked}); !errors.Is(err, errors.InvalidInput) {
		t.Fatal(err)
	}
}
