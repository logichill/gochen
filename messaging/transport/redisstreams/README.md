# Redis Streams Transport（参考实现说明）

> 状态：**示例/文档**，非框架内置实现  
> 完整参考实现已经下沉到本 README 的代码块中，不再以 `.go` 文件形式存在于仓库中。

## 设计目标

- 基于 `github.com/redis/go-redis/v9` 的 XADD/XREADGROUP 实现 `messaging.ITransport`。
- 支持消费者组 + 多实例，至少一次投递语义。
- 作为异步传输实现，用于配合 `messaging.MessageBus`。

## 使用方式概览

1. 在你的应用仓库中创建包（建议放在 `internal`）：

   ```bash
   internal/transport/redisstreams/
   ```

2. 从本 README 末尾的代码块中复制实现到你自己的包里，按项目需求裁剪。

3. 在应用组装处注入 Transport：

   ```go
   import (
       myredis "your-app/internal/transport/redisstreams"
       "gochen/messaging"
   )

   func newRedisTransport() messaging.ITransport {
       t, _ := myredis.NewTransport(myredis.Config{
           Addr:         "127.0.0.1:6379",
           StreamPrefix: "bus:",
           GroupName:    "gochen",
       })
       _ = t.Start(context.Background())
       return t
   }
   ```

4. 将该 Transport 传给 `messaging.NewMessageBus` / `command.NewCommandBus` 即可。

## 附录：参考实现完整代码

下面是一个基于 Redis Streams 的完整 Transport 实现示例，你可以直接复制到业务仓库，并根据需要调整包名与依赖：

```go
package redisstreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"gochen/logging"
	"gochen/messaging"
)

// client captures the subset of go-redis commands we rely on (for easier testing).
type client interface {
	XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd
	XReadGroup(ctx context.Context, a *redis.XReadGroupArgs) *redis.XStreamSliceCmd
	XAck(ctx context.Context, stream, group string, ids ...string) *redis.IntCmd
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) *redis.StatusCmd
	Close() error
}

// Config describes how the Redis Streams transport should connect/behave.
type Config struct {
	Client       redis.UniversalClient
	Addr         string
	Username     string
	Password     string
	DB           int
	StreamPrefix string
	GroupName    string
	ConsumerName string
	BlockTimeout time.Duration
	ReadCount    int64
	Logger       logging.Logger

	// 并发与背压配置
	MaxPublishConcurrency int           // 限制同时进行的 XADD 数，0 表示不限制
	MinReadBackoff        time.Duration // 订阅错误最小退避，默认 100ms
	MaxReadBackoff        time.Duration // 订阅错误最大退避，默认 5s
}

// Transport is a messaging.Transport backed by Redis Streams consumer groups.
type Transport struct {
	cfg       Config
	client    client
	ownClient bool
	logger    logging.Logger

	handlers      map[string][]messaging.IMessageHandler
	subscriptions map[string]bool

	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// 并发控制
	pubSem chan struct{}
}

// NewTransport constructs a Redis Streams transport.
func NewTransport(cfg Config) (*Transport, error) {
	if cfg.StreamPrefix == "" {
		cfg.StreamPrefix = "bus:"
	}
	if cfg.GroupName == "" {
		cfg.GroupName = "gochen"
	}
	if cfg.ConsumerName == "" {
		cfg.ConsumerName = "consumer-" + uuid.NewString()
	}
	if cfg.BlockTimeout <= 0 {
		cfg.BlockTimeout = 5 * time.Second
	}
	if cfg.ReadCount <= 0 {
		cfg.ReadCount = 10
	}

	var cl client
	var own bool
	if cfg.Client != nil {
		cl = cfg.Client
	} else {
		options := &redis.Options{Addr: cfg.Addr, Username: cfg.Username, Password: cfg.Password, DB: cfg.DB}
		redisClient := redis.NewClient(options)
		cl = redisClient
		own = true
	}
	if cl == nil {
		return nil, errors.New("redis client not configured")
	}

	if cfg.Logger == nil {
		cfg.Logger = logging.GetLogger().WithFields(logging.String("component", "transport.redisstreams"))
	}

	t := &Transport{
		cfg:           cfg,
		client:        cl,
		ownClient:     own,
		logger:        cfg.Logger,
		handlers:      make(map[string][]messaging.IMessageHandler),
		subscriptions: make(map[string]bool),
	}
	if t.cfg.MinReadBackoff <= 0 {
		t.cfg.MinReadBackoff = 100 * time.Millisecond
	}
	if t.cfg.MaxReadBackoff <= 0 {
		t.cfg.MaxReadBackoff = 5 * time.Second
	}
	if t.cfg.MaxPublishConcurrency > 0 {
		t.pubSem = make(chan struct{}, t.cfg.MaxPublishConcurrency)
	}
	return t, nil
}

// Publish writes a single message into the appropriate Stream.
func (t *Transport) Publish(ctx context.Context, message messaging.IMessage) error {
	return t.publish(ctx, message)
}

// PublishAll writes multiple messages.
func (t *Transport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	for _, msg := range messages {
		if err := t.publish(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (t *Transport) publish(ctx context.Context, message messaging.IMessage) error {
	if t.pubSem != nil {
		select {
		case t.pubSem <- struct{}{}:
			defer func() { <-t.pubSem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	values, err := encodeMessage(message)
	if err != nil {
		return err
	}
	stream := t.streamName(message.GetType())
	return t.client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: values}).Err()
}

// Subscribe registers a handler for a given message type.
func (t *Transport) Subscribe(messageType string, handler messaging.IMessageHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handlers[messageType] = append(t.handlers[messageType], handler)
	if t.running {
		t.startReaderLocked(messageType)
	}
	return nil
}

// Unsubscribe removes the handler for a message type (no-op if not found).
func (t *Transport) Unsubscribe(messageType string, handler messaging.IMessageHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	handlers := t.handlers[messageType]
	for i, h := range handlers {
		if h == handler {
			t.handlers[messageType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
	return nil
}

// Start begins background consumers per message type.
func (t *Transport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return fmt.Errorf("redis streams transport already running")
	}
	t.ctx, t.cancel = context.WithCancel(ctx)
	for mt := range t.handlers {
		t.startReaderLocked(mt)
	}
	t.running = true
	return nil
}

// Close stops consumers and optionally closes the redis client.
func (t *Transport) Close() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		if t.ownClient {
			return t.client.Close()
		}
		return nil
	}
	t.running = false
	cancel := t.cancel
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	t.wg.Wait()
	if t.ownClient {
		return t.client.Close()
	}
	return nil
}

// Stats returns basic handler/stream information.
func (t *Transport) Stats() messaging.TransportStats {
	t.mu.RLock()
	defer t.mu.RUnlock()
	handlerCount := 0
	types := make([]string, 0, len(t.handlers))
	for mt, hs := range t.handlers {
		handlerCount += len(hs)
		types = append(types, mt)
	}
	return messaging.TransportStats{
		Running:      t.running,
		HandlerCount: handlerCount,
		MessageTypes: types,
	}
}

func (t *Transport) startReaderLocked(messageType string) {
	if t.subscriptions[messageType] {
		return
	}
	t.subscriptions[messageType] = true
	t.wg.Add(1)
	go t.readLoop(messageType)
}

func (t *Transport) readLoop(messageType string) {
	defer t.wg.Done()
	stream := t.streamName(messageType)
	if err := t.ensureGroup(stream); err != nil {
		t.logger.Warn(t.ctx, "ensure group failed", logging.String("stream", stream), logging.Error(err))
	}
	args := &redis.XReadGroupArgs{
		Group:    t.cfg.GroupName,
		Consumer: t.cfg.ConsumerName,
		Streams:  []string{stream, ">"},
		Count:    t.cfg.ReadCount,
		Block:    t.cfg.BlockTimeout,
	}
	backoff := t.cfg.MinReadBackoff
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}
		streams, err := t.client.XReadGroup(t.ctx, args).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			t.logger.Warn(t.ctx, "XReadGroup failed", logging.Error(err))
			time.Sleep(backoff)
			backoff *= 2
			if backoff > t.cfg.MaxReadBackoff {
				backoff = t.cfg.MaxReadBackoff
			}
			continue
		}
		backoff = t.cfg.MinReadBackoff
		for _, s := range streams {
			for _, msg := range s.Messages {
				t.handleStreamMessage(stream, msg)
			}
		}
	}
}

func (t *Transport) handleStreamMessage(stream string, xmsg redis.XMessage) {
	var m messaging.Message
	if err := decodeMessage(xmsg.Values, &m); err != nil {
		t.logger.Error(t.ctx, "failed to decode message", logging.Error(err))
		_ = t.client.XAck(t.ctx, stream, t.cfg.GroupName, xmsg.ID)
		return
	}
	msgType := m.Type
	t.mu.RLock()
	handlers := append([]messaging.IMessageHandler(nil), t.handlers[msgType]...)
	t.mu.RUnlock()

	for _, h := range handlers {
		if err := h.Handle(t.ctx, &m); err != nil {
			t.logger.Error(t.ctx, "handler failed", logging.Error(err))
		}
	}

	_ = t.client.XAck(t.ctx, stream, t.cfg.GroupName, xmsg.ID)
}

func (t *Transport) ensureGroup(stream string) error {
	err := t.client.XGroupCreateMkStream(t.ctx, stream, t.cfg.GroupName, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (t *Transport) streamName(messageType string) string {
	return t.cfg.StreamPrefix + messageType
}

func encodeMessage(msg messaging.IMessage) (map[string]interface{}, error) {
	m, ok := msg.(*messaging.Message)
	if !ok {
		m = &messaging.Message{
			ID:        msg.GetID(),
			Type:      msg.GetType(),
			Timestamp: msg.GetTimestamp(),
			Payload:   msg.GetPayload(),
			Metadata:  msg.GetMetadata(),
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":        m.ID,
		"type":      m.Type,
		"ts":        m.Timestamp.UnixNano(),
		"payload":   string(data),
		"aggregate": fmt.Sprintf("%d:%s", m.GetAggregateID(), m.GetAggregateType()),
	}, nil
}

func decodeMessage(values map[string]interface{}, out *messaging.Message) error {
	raw, ok := values["payload"]
	if !ok {
		return errors.New("missing payload")
	}
	s, ok := raw.(string)
	if !ok {
		return errors.New("invalid payload type")
	}
	if err := json.Unmarshal([]byte(s), out); err != nil {
		return err
	}
	if tsRaw, ok := values["ts"]; ok {
		if tsNum, err := strconv.ParseInt(fmt.Sprint(tsRaw), 10, 64); err == nil {
			out.Timestamp = time.Unix(0, tsNum)
		}
	}
	return nil
}
```

