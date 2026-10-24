package subscription

import (
	"context"
	stderrors "errors"
	"time"

	"gochen/errors"
	"gochen/eventing/store"
	"gochen/observe/logging"
	"gochen/policy/retry"
)

// Run 启动主服务并阻塞运行。
//
// 说明：
// - Run 启动订阅循环，直到 ctx 取消或出现错误。
func (s *Subscription[ID]) Run(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.NewCode(errors.InvalidInput, "ctx is nil")
	}
	if err := s.loadPersistentCursor(ctx); err != nil {
		return err
	}

	s.logger.Info(ctx, "subscription started",
		logging.Int("batch_size", s.cfg.BatchSize),
		logging.Any("from_time", s.cfg.FromTime),
		logging.String("cursor", s.Cursor()))

	// Avoid per-iteration allocation from time.After in polling loops.
	pollTimer := time.NewTimer(0)
	if !pollTimer.Stop() {
		select {
		case <-pollTimer.C:
		default:
		}
	}
	defer pollTimer.Stop()

	pollFailures := 0
	for {
		select {
		case <-ctx.Done():
			s.logger.Info(ctx, "subscription stopped", logging.String("cursor", s.Cursor()))
			return ctx.Err()
		default:
		}

		opts := &store.StreamOptions{
			After:          s.Cursor(),
			Limit:          s.cfg.BatchSize,
			FromTime:       s.cfg.FromTime,
			Types:          append([]string(nil), s.cfg.Types...),
			AggregateTypes: append([]string(nil), s.cfg.AggregateTypes...),
		}

		res, err := s.store.StreamEvents(ctx, opts)
		if err != nil {
			pollFailures++
			if pollFailures > s.cfg.MaxPollRetries {
				s.logger.Error(ctx, "subscription stream poll exhausted retries",
					logging.Int("poll_failures", pollFailures),
					logging.String("cursor", s.Cursor()),
					logging.Error(err))
				return err
			}
			backoff := s.pollBackoff(pollFailures)
			s.logger.Warn(ctx, "subscription stream poll failed; backing off",
				logging.Int("attempt", pollFailures),
				logging.Duration("backoff", backoff),
				logging.String("cursor", s.Cursor()),
				logging.Error(err))
			if !pollTimer.Stop() {
				select {
				case <-pollTimer.C:
				default:
				}
			}
			pollTimer.Reset(backoff)
			select {
			case <-pollTimer.C:
				continue
			case <-ctx.Done():
				s.logger.Info(ctx, "subscription stopped", logging.String("cursor", s.Cursor()))
				return ctx.Err()
			}
		}
		pollFailures = 0
		if res == nil || len(res.Events) == 0 {
			if res != nil && res.NextCursor != "" {
				if s.cfg.CursorStore == nil {
					s.setCursor(res.NextCursor)
				}
				if err := s.flushCursor(ctx, res.NextCursor); err != nil {
					return err
				}
			}
			if !pollTimer.Stop() {
				select {
				case <-pollTimer.C:
				default:
				}
			}
			pollTimer.Reset(s.cfg.PollInterval)
			select {
			case <-pollTimer.C:
				continue
			case <-ctx.Done():
				s.logger.Info(ctx, "subscription stopped", logging.String("cursor", s.Cursor()))
				return ctx.Err()
			}
		}

		var lastSuccessfulCursor string
		for i := range res.Events {
			if err := s.handleEvent(ctx, res.Events[i]); err != nil {
				if flushErr := s.flushCursor(ctx, lastSuccessfulCursor); flushErr != nil {
					return stderrors.Join(err, flushErr)
				}
				return err
			}
			lastSuccessfulCursor = eventCursorAt(res, i)
			if s.cfg.CursorStore == nil {
				s.setCursor(lastSuccessfulCursor)
			}
		}
		if res.NextCursor != "" {
			lastSuccessfulCursor = res.NextCursor
			// 无 CursorStore 时 flushCursor 直接 no-op，必须手动推进内存游标，
			// 否则下游持久化位点缺失场景下（如被过滤区间）重复扫同一段流。
			if s.cfg.CursorStore == nil {
				s.setCursor(lastSuccessfulCursor)
			}
		}
		if err := s.flushCursor(ctx, lastSuccessfulCursor); err != nil {
			return err
		}
	}
}

// pollBackoff 计算第 attempt 次轮询失败后的退避时长：基础值指数增长并封顶到 32×。
func (s *Subscription[ID]) pollBackoff(attempt int) time.Duration {
	base := s.cfg.PollRetryBackoff
	if base <= 0 {
		base = s.cfg.PollInterval
	}
	if base <= 0 {
		return 0
	}
	return retry.ComputeDelay(retry.Config{
		InitialDelay:  base,
		BackoffFactor: 2,
		MaxDelay:      durationMulCap(base, 32),
		JitterRatio:   0,
	}, attempt)
}

func durationMulCap(base time.Duration, factor int64) time.Duration {
	if base <= 0 || factor <= 0 {
		return 0
	}
	const maxDuration = time.Duration(1<<63 - 1)
	mul := time.Duration(factor)
	if base > maxDuration/mul {
		return maxDuration
	}
	return base * mul
}
