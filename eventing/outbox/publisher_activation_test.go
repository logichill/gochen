package outbox

import (
	"context"

	"gochen/eventing/bus"
	"gochen/eventing/registry"
	"gochen/eventing/upcast"
	"gochen/observe/logging"
)

// Existing publisher behavior tests explicitly accept the standalone Core
// task executor. Authorization and validation failures use the public constructors.
func activateTestPublisher(publisher IOutboxPublisher) error {
	if err := publisher.SetTaskExecutor(func(ctx context.Context, _ string, handler func(context.Context) error) error { return handler(ctx) }); err != nil {
		return err
	}
	if err := publisher.TaskRegistry().SealHandlers(); err != nil {
		return err
	}
	return publisher.TaskRegistry().ActivateHandlers()
}

func newTestPublisher[ID comparable](repo IOutboxRepository[ID], eventBus bus.IEventBus, cfg OutboxConfig, logger logging.ILogger, reg *registry.Registry, upgraders *upcast.UpgraderRegistry) (*Publisher[ID], error) {
	publisher, err := NewPublisher(repo, eventBus, cfg, logger, reg, upgraders)
	if err != nil {
		return nil, err
	}
	if err := activateTestPublisher(publisher); err != nil {
		return nil, err
	}
	return publisher, nil
}

func newTestParallelPublisher[ID comparable](repo IOutboxRepository[ID], eventBus bus.IEventBus, cfg OutboxConfig, logger logging.ILogger, workers int, reg *registry.Registry, upgraders *upcast.UpgraderRegistry) (*ParallelPublisher[ID], error) {
	publisher, err := NewParallelPublisher(repo, eventBus, cfg, logger, workers, reg, upgraders)
	if err != nil {
		return nil, err
	}
	if err := activateTestPublisher(publisher); err != nil {
		return nil, err
	}
	return publisher, nil
}

func newTestClaimTasks[ID comparable](repo IOutboxRepository[ID]) (*publisherTasks[ID], error) {
	tasks, err := newPublisherTasks("test.outbox", repo, logging.NewNoopLogger())
	if err != nil {
		return nil, err
	}
	if err := tasks.configure(func(ctx context.Context, _ string, handler func(context.Context) error) error { return handler(ctx) }); err != nil {
		return nil, err
	}
	if err := tasks.registry.SealHandlers(); err != nil {
		return nil, err
	}
	if err := tasks.registry.ActivateHandlers(); err != nil {
		return nil, err
	}
	return tasks, nil
}
