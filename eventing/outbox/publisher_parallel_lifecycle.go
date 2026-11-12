package outbox

import (
	"context"

	"time"

	"gochen/errors"

	"gochen/observe/logging"
)

// closeWorkChannels 关闭所有 worker 输入通道，通知其退出消费循环。
// 调用方必须持有 dispatchMu；关闭后置位 workClosed，供 PublishPending 复查停止状态。
func (p *ParallelPublisher[ID]) closeWorkChannels() {
	p.workChOnce.Do(func() {
		for i := range p.workChs {
			close(p.workChs[i])
		}
	})
	p.workClosed = true
}

// Start 启动 worker、拉取循环和清理循环。
func (p *ParallelPublisher[ID]) Start(ctx context.Context) error {
	if p == nil {
		return errors.NewCode(errors.InvalidInput, "publisher cannot be nil")
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := validatePublisherDependencies(p.repo, p.bus); err != nil {
		return err
	}

	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return errors.NewCode(errors.InvalidInput, "publisher has been stopped; create a new instance")
	}
	if p.started {
		p.mu.Unlock()
		return nil
	}
	if err := p.tasks.checkActive(); err != nil {
		p.mu.Unlock()
		return err
	}
	runCtx, runCancel := context.WithCancel(ctx)
	batchOps := p.batchOps
	var markCh chan markOp[ID]
	var markStopCh chan struct{}
	var markDoneCh chan struct{}
	if batchOps != nil {
		markCap := p.workerCount * p.cfg.BatchSize
		if markCap < p.workerCount*4 {
			markCap = p.workerCount * 4
		}
		if markCap > 10000 {
			markCap = 10000
		}
		markCh = make(chan markOp[ID], markCap)
		markStopCh = make(chan struct{})
		markDoneCh = make(chan struct{})
		p.markCh = markCh
		p.markStopCh = markStopCh
		p.markDoneCh = markDoneCh
		p.markStopped = false
	}
	p.wg.Add(p.workerCount + 2)
	p.started = true
	p.runCancel = runCancel
	p.runContext = runCtx
	p.freezeCoreLocked()
	trackedLaunches := 0
	markLoopLaunched := false
	launchTracked := func(name string) error {
		if err := p.tasks.registry.Go(runCtx, p.tasks.prefix+name); err != nil {
			return err
		}
		trackedLaunches++
		return nil
	}
	abortStart := func(startErr error) error {
		for i := trackedLaunches; i < p.workerCount+2; i++ {
			p.wg.Done()
		}
		if !markLoopLaunched {
			p.markDoneCh = nil
		}
		p.started = false
		p.stopped = true
		p.stopOnce.Do(func() { close(p.stopCh) })
		runCancel()
		p.mu.Unlock()
		p.dispatchMu.Lock()
		p.closeWorkChannels()
		p.dispatchMu.Unlock()
		return errors.Join(errors.Wrap(startErr, errors.Internal, "start outbox parallel publisher task failed"), p.startFinalizeStop(runCtx))
	}

	if markCh != nil && markDoneCh != nil {
		if err := p.tasks.registry.Go(runCtx, p.tasks.prefix+"mark_loop"); err != nil {
			return abortStart(err)
		}
		markLoopLaunched = true
	}
	for i := 0; i < p.workerCount; i++ {
		if err := launchTracked(workerTaskName(i)); err != nil {
			return abortStart(err)
		}
	}
	if err := launchTracked("fetch_loop"); err != nil {
		return abortStart(err)
	}
	if err := launchTracked("cleanup_loop"); err != nil {
		return abortStart(err)
	}
	if err := p.tasks.registry.Go(runCtx, p.tasks.prefix+"context_watcher"); err != nil {
		return abortStart(err)
	}

	p.mu.Unlock()
	return nil
}

func (p *ParallelPublisher[ID]) requestStopFromRunContext() error {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return nil
	}
	runCtx := p.runContext
	p.started = false
	p.stopped = true
	p.mu.Unlock()

	p.stopOnce.Do(func() { close(p.stopCh) })
	return p.startFinalizeStop(runCtx)
}

// Stop 请求并行发布器停止，并等待 worker 与批量标记协程收尾。
func (p *ParallelPublisher[ID]) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	p.mu.Lock()
	if !p.started && !p.stopped {
		p.mu.Unlock()
		return nil
	}
	firstStop := !p.stopped
	runCancel := p.runCancel
	runCtx := p.runContext
	if firstStop {
		if p.markCh != nil {
			p.markStopCtx = ctx
		}
		p.started = false
		p.stopped = true
	}
	stopDone := p.stopDone
	p.mu.Unlock()

	if firstStop {
		p.stopOnce.Do(func() { close(p.stopCh) })
		if err := p.startFinalizeStop(runCtx); err != nil {
			return err
		}
	}

	select {
	case <-stopDone:
		return p.tasks.registry.Stop(ctx)
	default:
	}
	select {
	case <-stopDone:
		return p.tasks.registry.Stop(ctx)
	case <-ctx.Done():
		select {
		case <-stopDone:
			return p.tasks.registry.Stop(ctx)
		default:
		}
		if runCancel != nil {
			runCancel()
		}
		p.stopMarkLoop()
		if ctx.Err() == context.DeadlineExceeded {
			return errors.NewCode(errors.Timeout, "outbox parallel publisher stop timeout").WithContext("cause", ctx.Err().Error())
		}
		return ctx.Err()
	}
}

func (p *ParallelPublisher[ID]) finishStop() {
	// fetchLoop closes worker inputs; workers drain claimed entries before final mark flush.
	p.wg.Wait()
	if p.markCh != nil {
		p.markChOnce.Do(func() { close(p.markCh) })
	}
	if p.markDoneCh != nil {
		<-p.markDoneCh
	}
	p.mu.Lock()
	runCancel := p.runCancel
	p.runCancel = nil
	p.mu.Unlock()
	if runCancel != nil {
		runCancel()
	}
	close(p.stopDone)
}

func (p *ParallelPublisher[ID]) stopMarkLoop() {
	if p == nil {
		return
	}
	p.markSendMu.Lock()
	p.mu.Lock()
	p.markStopped = true
	stopCh := p.markStopCh
	p.mu.Unlock()
	if stopCh != nil {
		p.markStopOnce.Do(func() {
			close(stopCh)
		})
	}
	p.markSendMu.Unlock()
}

func (p *ParallelPublisher[ID]) tryEnqueueMark(ctx context.Context, op markOp[ID]) bool {
	if p == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	p.markSendMu.Lock()
	defer p.markSendMu.Unlock()
	p.mu.Lock()
	batchOps := p.batchOps
	markCh := p.markCh
	stopped := p.markStopped
	p.mu.Unlock()
	if batchOps == nil || markCh == nil || stopped {
		return false
	}
	select {
	case markCh <- op:
		return true
	default:
		return false
	}
}

const defaultParallelPublisherStopTimeout = 30 * time.Second

// cleanupLoop 定期清理已发布的记录。
func (p *ParallelPublisher[ID]) cleanupLoop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			if err := p.core().cleanupPublished(ctx, time.Now()); err != nil {
				p.log.Error(ctx, "cleanup published failed", logging.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}
