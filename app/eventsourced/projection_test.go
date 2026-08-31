package eventsourced

import (
	"context"
	"testing"

	"gochen/testkit/require"

	"gochen/eventing"
	"gochen/eventing/projection"
)

func TestEventSourcedProjectionSupportsStringAggregateID(t *testing.T) {
	var handled []string
	var rebuilt []eventing.Event[string]

	p, err := NewEventSourcedProjection[string, string](EventSourcedProjectionOption[string, string]{
		Name:       "string_projection",
		EventTypes: []string{"NameChanged"},
		Handle: func(ctx context.Context, payload string) error {
			handled = append(handled, payload)
			return nil
		},
		Rebuild: func(ctx context.Context, events []eventing.Event[string]) error {
			rebuilt = append(rebuilt, events...)
			return nil
		},
	})
	require.NoError(t, err)

	var _ projection.IProjection[string] = p

	evt := newTestEvent[string]("account-1", "Account", "NameChanged", 1, "new-name")
	require.NoError(t, p.Handle(context.Background(), evt))
	require.Equal(t, []string{"new-name"}, handled)

	events := []eventing.Event[string]{*evt}
	require.NoError(t, p.Rebuild(context.Background(), events))
	require.Len(t, rebuilt, 1)
	require.Equal(t, "account-1", rebuilt[0].GetAggregateID())
}
