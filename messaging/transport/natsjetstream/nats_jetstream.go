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
	Logger        logging.Logger
	Conn          *nats.Conn

	// 可选：流参数
	Retention         string // workqueue|limits|interest（默认 workqueue）
	MaxBytes          int64  // 0 表示不设置
	Replicas          int    // 0 表示默认
	MaxMsgsPerSubject int64  // 每主题最大消息数，默认 -1
}

// Transport implements messaging.Transport on top of NATS JetStream.
type Transport struct {
	cfg      Config
	logger   logging.Logger
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
		nats.Durable(durable),
		nats.AckWait(t.cfg.AckWait),
		nats.MaxAckPending(t.cfg.MaxAckPending))
	if err != nil {
		return err
	}
	t.subs[messageType] = sub
	return nil
}

func (t *Transport) handleMessage(defaultType string) nats.MsgHandler {
	return func(msg *nats.Msg) {
		decoded, err := unmarshalMessage(msg.Data)
		if err != nil {
			t.logger.Warn(context.Background(), "decode nats message failed", logging.Error(err))
			_ = msg.Ack()
			return
		}
		if decoded.GetType() == "" {
			if m, ok := decoded.(*messaging.Message); ok {
				m.Type = defaultType
			}
		}
		t.dispatch(context.Background(), decoded)
		if err := msg.Ack(); err != nil {
			t.logger.Warn(context.Background(), "nats ack failed", logging.Error(err))
		}
	}
}

func (t *Transport) dispatch(ctx context.Context, message messaging.IMessage) {
	t.mu.RLock()
	exact := t.handlers[message.GetType()]
	wildcard := t.handlers["*"]
	handlers := make([]messaging.IMessageHandler, 0, len(exact)+len(wildcard))
	handlers = append(handlers, exact...)
	handlers = append(handlers, wildcard...)
	t.mu.RUnlock()

	for _, h := range handlers {
		_ = h.Handle(ctx, message)
	}
}

func (t *Transport) subjectName(messageType string) string {
	return t.cfg.SubjectPrefix + messageType
}

func marshalMessage(msg messaging.IMessage) ([]byte, error) {
	payload, err := json.Marshal(msg.GetPayload())
	if err != nil {
		return nil, err
	}
	metadata := msg.GetMetadata()
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	ts := msg.GetTimestamp()
	if ts.IsZero() {
		ts = time.Now()
	}
	return json.Marshal(struct {
		ID        string                 `json:"id"`
		Type      string                 `json:"type"`
		Timestamp int64                  `json:"timestamp"`
		Payload   json.RawMessage        `json:"payload"`
		Metadata  map[string]interface{} `json:"metadata"`
	}{ID: msg.GetID(), Type: msg.GetType(), Timestamp: ts.UnixNano(), Payload: payload, Metadata: metadata})
}

func unmarshalMessage(data []byte) (messaging.IMessage, error) {
	var wire struct {
		ID        string                 `json:"id"`
		Type      string                 `json:"type"`
		Timestamp int64                  `json:"timestamp"`
		Payload   json.RawMessage        `json:"payload"`
		Metadata  map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	var payload interface{}
	if len(wire.Payload) > 0 {
		if err := json.Unmarshal(wire.Payload, &payload); err != nil {
			return nil, err
		}
	}
	if wire.Metadata == nil {
		wire.Metadata = make(map[string]interface{})
	}
	msg := &messaging.Message{
		ID:        wire.ID,
		Type:      wire.Type,
		Timestamp: time.Unix(0, wire.Timestamp),
		Payload:   payload,
		Metadata:  wire.Metadata,
	}
	return msg, nil
}
