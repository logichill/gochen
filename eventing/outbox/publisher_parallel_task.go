package outbox

import (
	"context"
	"fmt"

	"gochen/errors"
)

func (p *ParallelPublisher[ID]) registerTasks() error {
	register := func(name string, handler func(context.Context) error) error {
		return p.tasks.registry.Register(p.tasks.prefix+name, handler)
	}
	if err := register("publish_pending", func(ctx context.Context) error { return p.tasks.execute(ctx, "publish_pending", p.publishPending) }); err != nil {
		return err
	}
	for i := 0; i < p.workerCount; i++ {
		workerID := i
		name := workerTaskName(i)
		if err := register(name, func(ctx context.Context) error {
			entered := false
			defer func() {
				if !entered {
					p.wg.Done()
				}
			}()
			return p.tasks.execute(ctx, name, func(ctx context.Context) error {
				entered = true
				p.worker(ctx, workerID, p.workChs[workerID])
				return nil
			})
		}); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		name    string
		handler func(context.Context)
	}{{"fetch_loop", p.fetchLoop}, {"cleanup_loop", p.cleanupLoop}} {
		if err := register(item.name, func(ctx context.Context) error {
			entered := false
			defer func() {
				if !entered {
					if item.name == "fetch_loop" {
						p.dispatchMu.Lock()
						p.closeWorkChannels()
						p.dispatchMu.Unlock()
					}
					p.wg.Done()
				}
			}()
			return p.tasks.execute(ctx, item.name, func(ctx context.Context) error { entered = true; item.handler(ctx); return nil })
		}); err != nil {
			return err
		}
	}
	if err := register("mark_loop", func(ctx context.Context) error {
		entered := false
		defer func() {
			if !entered && p.markDoneCh != nil {
				close(p.markDoneCh)
			}
		}()
		return p.tasks.execute(ctx, "mark_loop", func(ctx context.Context) error { entered = true; p.markLoop(ctx); return nil })
	}); err != nil {
		return err
	}
	if err := register("context_watcher", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return p.requestStopFromRunContext()
		case <-p.stopCh:
			return nil
		}
	}); err != nil {
		return err
	}
	return register("finalize_stop", func(context.Context) error { p.finishStop(); return nil })
}

func (p *ParallelPublisher[ID]) startFinalizeStop(ctx context.Context) error {
	var err error
	p.stopFinalizeOnce.Do(func() { err = p.tasks.registry.Go(context.WithoutCancel(ctx), p.tasks.prefix+"finalize_stop") })
	if err != nil {
		return errors.Wrap(err, errors.Internal, "start Outbox stop finalizer failed")
	}
	return nil
}

func workerTaskName(id int) string { return fmt.Sprintf("worker_%d", id) }
