package subscription

import (
	"context"
	stderrors "errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/observe/logging"
	"gochen/policy/retry"
)

const cursorFlushTimeout = 5 * time.Second

// HandlerFunc 订阅处理函数。
type HandlerFunc[ID comparable] func(ctx context.Context, evt eventing.Event[ID]) error

// ICursorStore 持久化订阅游标；游标语义是最后一条成功处理事件对应的后继游标。
// 默认事件流实现使用事件 ID；支持非 ID 游标的存储实现应通过 StreamResult.EventCursors 提供可持久化游标。
type ICursorStore interface {
	LoadCursor(ctx context.Context, subscriptionName string) (string, error)
	SaveCursor(ctx context.Context, subscriptionName string, cursor string) error
}

// Config 订阅配置。
type Config struct {
	// PollInterval 轮询间隔（当本轮无事件时等待）。
	PollInterval time.Duration

	// BatchSize 每次拉取的最大事件数。
	BatchSize int

	// MaxHandlerRetries 表示 handler 首次失败后的最大重试次数，0 表示不重试，负值沿用默认值 0。
	MaxHandlerRetries int

	// HandlerRetryBackoff 表示 handler 重试之间的等待时间，0 表示立即重试。
	HandlerRetryBackoff time.Duration

	// MaxPollRetries 表示 StreamEvents 轮询连续失败的最大容忍次数；超过后 Run 返回错误。
	// 用于在 DB 瞬时抖动时存活，同时对持久故障 fail-fast。任一次成功轮询会重置计数。
	// 0 表示不重试，负值沿用默认值（见 normalize）。
	MaxPollRetries int

	// PollRetryBackoff 表示轮询失败后的基础退避时长（按指数增长并封顶到 PollInterval 的若干倍）。
	// 0 表示沿用默认值（见 normalize）。
	PollRetryBackoff time.Duration

	// SkipFailedAfterMaxRetries 表示 handler 超过重试上限后调用 DeadLetterFunc、推进游标并继续消费。
	//
	// 默认 false，保持 at-least-once：失败事件不会被跳过，Run 返回 handler error。
	SkipFailedAfterMaxRetries bool

	// DeadLetterFunc 在 SkipFailedAfterMaxRetries=true 且 handler 仍失败时调用；返回错误会阻止游标推进并从 Run 返回。
	DeadLetterFunc func(err error, event eventing.IEvent) error

	// StartCursor 起始游标（从该事件 ID 之后开始消费）。
	StartCursor string

	// Name 是 CursorStore 中使用的订阅名称；配置 CursorStore 时必填。
	Name string

	// CursorStore 可选持久游标存储；nil 时仅使用内存游标。
	CursorStore ICursorStore

	// FromTime 起始时间（包含）。
	FromTime time.Time

	// Types 事件类型过滤（空表示不过滤）。
	Types []string

	// AggregateTypes 聚合类型过滤（空表示不过滤）。
	AggregateTypes []string

	// Logger 可选 logger（nil 时使用 component logger）。
	Logger logging.ILogger
}

// normalize 规范化输入。
func (c *Config) normalize() *Config {
	if c == nil {
		c = &Config{}
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 200 * time.Millisecond
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.MaxHandlerRetries < 0 {
		c.MaxHandlerRetries = 0
	}
	if c.HandlerRetryBackoff < 0 {
		c.HandlerRetryBackoff = 0
	}
	if c.MaxPollRetries < 0 {
		c.MaxPollRetries = 5
	}
	if c.PollRetryBackoff <= 0 {
		c.PollRetryBackoff = c.PollInterval
	}
	if c.Logger == nil {
		c.Logger = logging.ComponentLogger("eventing.subscription")
	}
	return c
}

// Subscription 定义Subscription。
type Subscription[ID comparable] struct {
	store   store.IEventStreamStore[ID]
	handler HandlerFunc[ID]
	cfg     *Config
	mu      sync.RWMutex
	cursor  string
	logger  logging.ILogger
}

type nonRetriableHandlerError struct {
	cause error
}

func (e nonRetriableHandlerError) Error() string {
	if e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e nonRetriableHandlerError) Unwrap() error {
	return e.cause
}

// New 创建Subscription[ID]。
func New[ID comparable](eventStore store.IEventStreamStore[ID], handler HandlerFunc[ID], cfg *Config) (*Subscription[ID], error) {
	if eventStore == nil {
		return nil, errors.NewCode(errors.InvalidInput, "event store cannot be nil")
	}
	if handler == nil {
		return nil, errors.NewCode(errors.InvalidInput, "handler cannot be nil")
	}
	cfg = cfg.normalize()
	if cfg.CursorStore != nil && cfg.Name == "" {
		return nil, errors.NewCode(errors.InvalidInput, "subscription name cannot be empty when cursor store is configured")
	}
	return &Subscription[ID]{
		store:   eventStore,
		handler: handler,
		cfg:     cfg,
		cursor:  cfg.StartCursor,
		logger:  cfg.Logger,
	}, nil
}

// Cursor 返回当前消费游标（最后一条成功处理事件对应的后继游标）。
func (s *Subscription[ID]) Cursor() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cursor
}

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
		}
		if err := s.flushCursor(ctx, lastSuccessfulCursor); err != nil {
			return err
		}
	}
}

func eventCursorAt[ID comparable](res *store.StreamResult[ID], i int) string {
	if res == nil || i < 0 || i >= len(res.Events) {
		return ""
	}
	if i < len(res.EventCursors) && res.EventCursors[i] != "" {
		return res.EventCursors[i]
	}
	if i == len(res.Events)-1 && res.NextCursor != "" {
		return res.NextCursor
	}
	return res.Events[i].GetID()
}

func (s *Subscription[ID]) flushCursor(ctx context.Context, cursor string) error {
	if cursor == "" || s.cfg.CursorStore == nil {
		return nil
	}
	flushCtx := cursorFlushContext(ctx)
	var cancel context.CancelFunc
	if flushCtx.Err() != nil {
		flushCtx, cancel = context.WithTimeout(context.Background(), cursorFlushTimeout)
	} else {
		flushCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), cursorFlushTimeout)
	}
	defer cancel()
	if err := s.cfg.CursorStore.SaveCursor(flushCtx, s.cfg.Name, cursor); err != nil {
		return err
	}
	s.setCursor(cursor)
	return nil
}

func cursorFlushContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *Subscription[ID]) loadPersistentCursor(ctx context.Context) error {
	if s.cfg.CursorStore == nil {
		return nil
	}
	cursor, err := s.cfg.CursorStore.LoadCursor(ctx, s.cfg.Name)
	if err != nil {
		return err
	}
	if cursor == "" {
		return nil
	}
	s.setCursor(cursor)
	return nil
}

func (s *Subscription[ID]) setCursor(cursor string) {
	s.mu.Lock()
	s.cursor = cursor
	s.mu.Unlock()
}

func (s *Subscription[ID]) handleEvent(ctx context.Context, evt eventing.Event[ID]) error {
	maxAttempts := s.cfg.MaxHandlerRetries + 1
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	attempts := 0
	cfg := retry.Config{
		MaxAttempts:   maxAttempts,
		InitialDelay:  s.cfg.HandlerRetryBackoff,
		BackoffFactor: 1,
		MaxDelay:      s.cfg.HandlerRetryBackoff,
		JitterRatio:   0,
		RetryIf: func(err error) bool {
			return !isNonRetriableHandlerError(err)
		},
	}
	err := retry.DoWithInfo(ctx, func(attemptCtx context.Context, attempt int) error {
		attempts = attempt
		handleErr := s.callHandler(attemptCtx, evt)
		if handleErr != nil && !isNonRetriableHandlerError(handleErr) && attempt < maxAttempts {
			s.logger.Warn(attemptCtx, "subscription handler retry",
				logging.String("event_id", evt.GetID()),
				logging.String("event_type", evt.GetType()),
				logging.Int("attempt", attempt),
				logging.Error(handleErr),
			)
		}
		return handleErr
	}, cfg)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if isNonRetriableHandlerError(err) || !s.cfg.SkipFailedAfterMaxRetries {
		return err
	}
	if s.cfg.DeadLetterFunc != nil {
		if dlqErr := s.cfg.DeadLetterFunc(err, &evt); dlqErr != nil {
			s.logger.Error(ctx, "subscription dead-letter hook failed",
				logging.String("event_id", evt.GetID()),
				logging.String("event_type", evt.GetType()),
				logging.Int("attempts", attempts),
				logging.Error(dlqErr),
			)
			return errors.Wrap(dlqErr, errors.Dependency, "subscription dead-letter hook failed").
				WithContext("event_id", evt.GetID())
		}
	}
	s.logger.Error(ctx, "subscription event dead-lettered after handler retries",
		logging.String("event_id", evt.GetID()),
		logging.String("event_type", evt.GetType()),
		logging.Int("attempts", attempts),
		logging.Error(err),
	)
	return nil
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

func (s *Subscription[ID]) callHandler(ctx context.Context, evt eventing.Event[ID]) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicValue := fmt.Sprint(recovered)
			stack := string(debug.Stack())
			err = nonRetriableHandlerError{cause: errors.NewCode(errors.Internal, "subscription handler panicked").
				WithContext("panic", panicValue).
				WithContext("panic_stack", stack).
				WithContext("event_id", evt.GetID()).
				WithContext("event_type", evt.GetType())}
			s.logger.Error(ctx, "subscription handler panicked",
				logging.String("event_id", evt.GetID()),
				logging.String("event_type", evt.GetType()),
				logging.Any("panic", recovered),
				logging.String("stack", stack),
			)
		}
	}()
	return s.handler(ctx, evt)
}

func isNonRetriableHandlerError(err error) bool {
	var target nonRetriableHandlerError
	return stderrors.As(err, &target)
}
