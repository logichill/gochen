package command

import (
	"context"
	"testing"

	"gochen/testkit/require"

	"gochen/errors"
)

func TestCommandHandlerFunc_NilReturnsError(t *testing.T) {
	var handler CommandHandlerFunc
	err := handler.AsMessageHandler("test").Handle(
		context.Background(),
		NewCommand("cmd-1", "test", "1", "test", nil),
	)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestTypedCommandHandler_NilFunctionReturnsError(t *testing.T) {
	handler := NewTypedCommandHandler[struct{}]("test", nil)
	err := handler.Handle(
		context.Background(),
		NewCommand("cmd-1", "test", "1", "test", struct{}{}),
	)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
}

func TestTypedCommandHandler_NilReceiverReturnsError(t *testing.T) {
	var handler *TypedCommandHandler[struct{}]
	err := handler.Handle(
		context.Background(),
		NewCommand("cmd-1", "test", "1", "test", struct{}{}),
	)
	require.Error(t, err)
	require.True(t, errors.Is(err, errors.InvalidInput))
	require.Empty(t, handler.Type())
}
