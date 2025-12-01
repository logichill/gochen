# NATS JetStream Transport（参考实现说明）

> 状态：**示例/文档**，非框架内置实现  
> 完整参考实现已经下沉到本 README 的代码块中，不再以 `.go` 文件形式存在于仓库中。

## 设计背景

`gochen` 的消息总线通过 `messaging.ITransport` 抽象到底层传输。  
核心仓库只内置了：

- `messaging/transport/memory`：内存异步队列；
- `messaging/transport/sync`：同步直呼 Handler。

像 NATS JetStream 这种**具体基础设施绑定**的实现，最好放在业务仓库（或单独 infra 仓库）中，以避免：

- 所有使用 gochen 的项目被迫依赖 `github.com/nats-io/nats.go`；
- 框架对连接方式、认证、拓扑结构做过度假设。

因此，这里只保留文档 + 完整代码示例，方便你在自己的项目里复制/裁剪。

## 使用方式概览

1. 在你的应用仓库中创建包（建议放在 `internal`）：

   ```bash
   internal/transport/natsjetstream/
   ```

2. 从本 README 末尾的“参考实现完整代码”代码块中复制实现到你自己的包里，按项目需求裁剪。

3. 在应用组装处注入 Transport：

   ```go
   import (
       mynats "your-app/internal/transport/natsjetstream"
       "gochen/messaging"
   )

   func newNATSTransport() messaging.ITransport {
       t := mynats.NewTransport(mynats.Config{
           URL:           nats.DefaultURL,
           Stream:        "GOCHEN",
           SubjectPrefix: "bus.",
       })
       _ = t.Start(context.Background())
       return t
   }
   ```

4. 将该 Transport 传给 `messaging.NewMessageBus` / `command.NewCommandBus` 即可。

## 附录：参考实现完整代码

下面是一个基于 `github.com/nats-io/nats.go` 的完整 JetStream Transport 实现示例，你可以直接复制到业务仓库，并根据需要调整包名与依赖：

```go
package natsjetstream

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"gochen/logging"
	"gochen/messaging"
)

// Config configures the JetStream transport.
type Config struct {
	URL           string
	Stream        string
	SubjectPrefix string
	DurablePrefix string
	AckWait       time.Duration
	MaxAckPending int
	Logger        logging.ILogger
	Conn          *nats.Conn

	// 可选：流参数
	Retention         string // workqueue|limits|interest（默认 workqueue）
	MaxBytes          int64  // 0 表示不设置
	Replicas          int    // 0 表示默认
	MaxMsgsPerSubject int64  // 每主题最大消息数，默认 -1
}

// Transport implements messaging.ITransport on top of NATS JetStream.
type Transport struct {
	cfg      Config
	logger   logging.ILogger
	conn     *nats.Conn
	js       nats.JetStreamContext
	ownsConn bool

	handlers map[string][]messaging.IMessageHandler
	subs     map[string]*nats.Subscription

	mu      sync.RWMutex
	running bool
}

// NewTransport builds a JetStream transport.
func NewTransport(cfg Config) *Transport {
	if cfg.Stream == "" {
		cfg.Stream = "GOCHEN"
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = "bus."
	}
	if cfg.DurablePrefix == "" {
		cfg.DurablePrefix = "gochen-"
	}
	if cfg.AckWait <= 0 {
		cfg.AckWait = 30 * time.Second
	}
	if cfg.MaxAckPending <= 0 {
		cfg.MaxAckPending = 1024
	}
	if cfg.Logger == nil {
		cfg.Logger = logging.GetLogger().WithFields(logging.String("component", "transport.nats"))
	}
	return &Transport{
		cfg:      cfg,
		logger:   cfg.Logger,
		handlers: make(map[string][]messaging.IMessageHandler),
		subs:     make(map[string]*nats.Subscription),
	}
}

func (t *Transport) Publish(ctx context.Context, message messaging.IMessage) error {
	t.mu.RLock()
	js := t.js
	running := t.running
	t.mu.RUnlock()
	if !running || js == nil {
		return errors.New("nats transport not running")
	}
	data, err := marshalMessage(message)
	if err != nil {
		return err
	}
	subject := t.subjectName(message.GetType())
	_, err = js.Publish(subject, data)
	return err
}

func (t *Transport) PublishAll(ctx context.Context, messages []messaging.IMessage) error {
	for _, msg := range messages {
		if err := t.Publish(ctx, msg); err != nil {
			return err
		}
	}
	return nil
}

func (t *Transport) Subscribe(messageType string, handler messaging.IMessageHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handlers[messageType] = append(t.handlers[messageType], handler)
	if t.running {
		if err := t.subscribeLocked(messageType); err != nil {
			return err
		}
	}
	return nil
}

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
	if len(t.handlers[messageType]) == 0 {
		if sub, ok := t.subs[messageType]; ok {
			_ = sub.Drain()
			delete(t.subs, messageType)
		}
	}
	return nil
}

func (t *Transport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return errors.New("nats transport already running")
	}
	if err := t.ensureConnection(); err != nil {
		return err
	}
	if err := t.ensureStream(); err != nil {
		return err
	}
	for mt := range t.handlers {
		if err := t.subscribeLocked(mt); err != nil {
			return err
		}
	}
	t.running = true
	return nil
}

func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		if t.ownsConn && t.conn != nil {
			t.conn.Close()
		}
		return nil
	}
	t.running = false
	for mt, sub := range t.subs {
		_ = sub.Drain()
		delete(t.subs, mt)
	}
	if t.ownsConn && t.conn != nil {
		t.conn.Close()
	}
	t.conn = nil
	t.js = nil
	return nil
}

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

func (t *Transport) ensureConnection() error {
	if t.conn != nil && t.js != nil {
		return nil
	}
	if t.cfg.Conn != nil {
		t.conn = t.cfg.Conn
	} else {
		if t.cfg.URL == "" {
			t.cfg.URL = nats.DefaultURL
		}
		conn, err := nats.Connect(t.cfg.URL)
		if err != nil {
			return err
		}
		t.conn = conn
		t.ownsConn = true
	}
	js, err := t.conn.JetStream()
	if err != nil {
		return err
	}
	t.js = js
	return nil
}

func (t *Transport) ensureStream() error {
	_, err := t.js.StreamInfo(t.cfg.Stream)
	if err == nil {
		return nil
	}
	if err != nil && !errors.Is(err, nats.ErrStreamNotFound) && !strings.Contains(err.Error(), "stream not found") {
		return err
	}
	// 组装流配置
	retention := nats.WorkQueuePolicy
	switch strings.ToLower(t.cfg.Retention) {
	case "limits":
		retention = nats.LimitsPolicy
	case "interest":
		retention = nats.InterestPolicy
	}
	sc := &nats.StreamConfig{
		Name:              t.cfg.Stream,
		Subjects:          []string{t.cfg.SubjectPrefix + ">"},
		Retention:         retention,
		MaxMsgsPerSubject: -1,
	}
	if t.cfg.MaxMsgsPerSubject != 0 {
		sc.MaxMsgsPerSubject = t.cfg.MaxMsgsPerSubject
	}
	if t.cfg.MaxBytes > 0 {
		sc.MaxBytes = t.cfg.MaxBytes
	}
	if t.cfg.Replicas > 0 {
		sc.Replicas = t.cfg.Replicas
	}
	_, err = t.js.AddStream(sc)
	return err
}

func (t *Transport) subscribeLocked(messageType string) error {
	if _, exists := t.subs[messageType]; exists {
		return nil
	}
	subject := t.subjectName(messageType)
	durable := t.cfg.DurablePrefix + messageType
	sub, err := t.js.QueueSubscribe(subject, durable, t.handleMessage(messageType),
		nats.ManualAck(),
		nats.AckWait(t.cfg.AckWait),
		nats.Durable(durable),
		nats.MaxAckPending(t.cfg.MaxAckPending),
	)
	if err != nil {
		return err
	}
	t.subs[messageType] = sub
	return nil
}

func (t *Transport) handleMessage(messageType string) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var m messaging.Message
		if err := json.Unmarshal(msg.Data, &m); err != nil {
			t.logger.Error(context.Background(), "failed to unmarshal message", logging.Error(err))
			_ = msg.Nak()
			return
		}
		t.mu.RLock()
		handlers := append([]messaging.IMessageHandler(nil), t.handlers[messageType]...)
		t.mu.RUnlock()
		ctx := context.Background()
		for _, h := range handlers {
			if err := h.Handle(ctx, &m); err != nil {
				t.logger.Error(ctx, "handler failed", logging.Error(err))
			}
		}
		_ = msg.Ack()
	}
}

func (t *Transport) subjectName(messageType string) string {
	return t.cfg.SubjectPrefix + messageType
}

func marshalMessage(msg messaging.IMessage) ([]byte, error) {
	m, ok := msg.(*messaging.Message)
	if !ok {
		m = &messaging.Message{
			ID:        msg.GetID(),
			Type:      msg.GetType(),
			Timestamp: msg.GetTimestamp(),
			.Payload:   msg.GetPayload(),
			Metadata:  msg.GetMetadata(),
		}
	}
	return json.Marshal(m)
}
```
