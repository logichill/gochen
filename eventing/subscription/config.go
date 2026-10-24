package subscription

import (
	"time"

	"gochen/eventing"
	"gochen/observe/logging"
)

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
	// 游标在每个事件成功后推进；handler 失败时停在最后成功事件，
	// 下次运行从失败事件继续（at-least-once）。
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
