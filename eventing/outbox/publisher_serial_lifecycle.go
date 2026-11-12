package outbox

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/observe/logging"
)

// Start 启动后台轮询循环；同一个 publisher 只允许启动并停止一次。
func (p *Publisher[ID]) Start(ctx context.Context) error {
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
	p.started = true
	p.freezeCoreLocked()
	runCtx, cancel := context.WithCancel(ctx)
	p.runCancel = cancel
	p.mu.Unlock()

	if err := p.tasks.registry.Go(runCtx, p.tasks.prefix+"poll_loop"); err != nil {
		cancel()
		p.mu.Lock()
		p.started = false
		p.runCancel = nil
		p.coreReady.Store(0)
		p.mu.Unlock()
		return errors.Wrap(err, errors.Internal, "start outbox publisher loop failed")
	}
	return nil
}

// Stop 请求后台循环退出，并等待当前一次发布流程收尾。
func (p *Publisher[ID]) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}

	var cancel context.CancelFunc
	p.mu.Lock()
	if !p.started && !p.stopped {
		// 未启动即 Stop：不设终态，后续仍可 Start 或手动 PublishPending。
		// 与 ParallelPublisher、README 生命周期承诺（"启动前 Stop 不影响后续
		// 启动"）保持一致；置为不可逆终态会破坏防御性 defer Stop 与初始化
		// 重试流程，且与并行发布器语义分叉。
		p.mu.Unlock()
		return nil
	}
	if p.stopped {
		cancel = p.runCancel
		p.runCancel = nil
		p.mu.Unlock()
	} else {
		p.started = false
		p.stopped = true
		cancel = p.runCancel
		p.runCancel = nil
		p.mu.Unlock()
	}

	if cancel != nil {
		cancel()
	}
	p.stopOnce.Do(func() { close(p.stopCh) })
	if err := p.waitForStop(ctx); err != nil {
		return err
	}
	if p.tasks != nil {
		return p.tasks.registry.Stop(ctx)
	}
	return nil
}

func (p *Publisher[ID]) waitForStop(ctx context.Context) error {
	select {
	case <-p.stopDone:
		return nil
	default:
	}
	select {
	case <-p.stopDone:
		return nil
	case <-ctx.Done():
		select {
		case <-p.stopDone:
			return nil
		default:
		}
		if ctx.Err() == context.DeadlineExceeded {
			return errors.NewCode(errors.Timeout, "outbox publisher stop timeout").WithContext("cause", ctx.Err().Error())
		}
		return ctx.Err()
	}
}

// loop 按配置周期重复执行单次发布，并按清理周期删除已发布记录。
func (p *Publisher[ID]) loop(ctx context.Context) {
	publishTicker := time.NewTicker(p.cfg.PublishInterval)
	cleanupTicker := time.NewTicker(p.cfg.CleanupInterval)
	defer func() {
		publishTicker.Stop()
		cleanupTicker.Stop()
	}()
	for {
		select {
		case <-p.stopCh:
			return
		case <-publishTicker.C:
			// 用函数作用域 + defer 释放锁：processOnce 发生 panic 时也能解锁，
			// 避免 panic 展开到顶层 defer 时再次 Lock 造成永久自锁死锁。
			// 发布后清理不在 processMu 内同步执行：删除可能分批耗时，
			// 长时间持锁会阻塞 PublishPending 与后续轮询；改为独立 goroutine，
			// 并用 cleanupMu.TryLock 保证同时至多一个清理在跑（忙时直接跳过）。
			func() {
				p.processMu.Lock()
				defer p.processMu.Unlock()
				err := p.processOnce(ctx)
				if err == nil && p.lastProcessPublished {
					p.spawnCleanupAfterSuccessfulPublish(ctx)
				}
				if err != nil {
					p.log.Error(ctx, "outbox processOnce failed in loop", logging.Error(err))
				}
			}()
		case <-cleanupTicker.C:
			if err := p.cleanupPublished(ctx, time.Now()); err != nil {
				p.log.Error(ctx, "outbox delete published failed", logging.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func (p *Publisher[ID]) runPollLoop(ctx context.Context) error {
	defer func() {
		p.mu.Lock()
		p.started, p.stopped = false, true
		cancel := p.runCancel
		p.runCancel = nil
		p.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		close(p.doneCh)
		p.processMu.Lock()
		p.stopDoneOnce.Do(func() { close(p.stopDone) })
		p.processMu.Unlock()
	}()
	return p.tasks.execute(ctx, "poll_loop", func(ctx context.Context) error { p.loop(ctx); return nil })
}
