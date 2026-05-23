package subscription

import (
	"context"
	"time"

	"gochen/errors"
	"gochen/eventing"
	"gochen/eventing/store"
	"gochen/logging"
)

// HandlerFunc 订阅处理函数。
type HandlerFunc[ID comparable] func(ctx context.Context, evt eventing.Event[ID]) error

// Config 订阅配置。
type Config struct {
	// PollInterval 轮询间隔（当本轮无事件时等待）。
	PollInterval time.Duration

	// BatchSize 每次拉取的最大事件数。
	BatchSize int

	// StartCursor 起始游标（从该事件 ID 之后开始消费）。
	StartCursor string

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
	cursor  string
	logger  logging.ILogger
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
	return &Subscription[ID]{
		store:   eventStore,
		handler: handler,
		cfg:     cfg,
		cursor:  cfg.StartCursor,
		logger:  cfg.Logger,
	}, nil
}

// Cursor 返回当前消费游标（最后一条成功处理的事件 ID）。
func (s *Subscription[ID]) Cursor() string { return s.cursor }

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

	s.logger.Info(ctx, "subscription started",
		logging.Int("batch_size", s.cfg.BatchSize),
		logging.Any("from_time", s.cfg.FromTime),
		logging.String("cursor", s.cursor))

	// Avoid per-iteration allocation from time.After in polling loops.
	pollTimer := time.NewTimer(0)
	if !pollTimer.Stop() {
		select {
		case <-pollTimer.C:
		default:
		}
	}
	defer pollTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info(ctx, "subscription stopped", logging.String("cursor", s.cursor))
			return ctx.Err()
		default:
		}

		opts := &store.StreamOptions{
			After:          s.cursor,
			Limit:          s.cfg.BatchSize,
			FromTime:       s.cfg.FromTime,
			Types:          append([]string(nil), s.cfg.Types...),
			AggregateTypes: append([]string(nil), s.cfg.AggregateTypes...),
		}

		res, err := s.store.StreamEvents(ctx, opts)
		if err != nil {
			return err
		}
		if res == nil || len(res.Events) == 0 {
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
				s.logger.Info(ctx, "subscription stopped", logging.String("cursor", s.cursor))
				return ctx.Err()
			}
		}

		for i := range res.Events {
			if err := s.handler(ctx, res.Events[i]); err != nil {
				return err
			}
		}
		if res.NextCursor != "" {
			s.cursor = res.NextCursor
		}
	}
}
