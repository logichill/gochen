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

// PublishAll writes messages sequentially. Redis Streams does not support multi append.
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
		res, err := t.client.XReadGroup(t.ctx, args).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			t.logger.Warn(t.ctx, "xreadgroup failed", logging.Duration("backoff", backoff), logging.Error(err))
			time.Sleep(backoff)
			backoff *= 2
			if backoff > t.cfg.MaxReadBackoff {
				backoff = t.cfg.MaxReadBackoff
			}
			continue
		}
		backoff = t.cfg.MinReadBackoff
		for _, streamRes := range res {
			for _, entry := range streamRes.Messages {
				msg, decodeErr := decodeMessage(entry)
				if decodeErr != nil {
					t.logger.Warn(t.ctx, "decode redis stream entry failed", logging.Error(decodeErr))
					_ = t.client.XAck(t.ctx, streamRes.Stream, t.cfg.GroupName, entry.ID).Err()
					continue
				}
				t.dispatch(t.ctx, msg)
				if ackErr := t.client.XAck(t.ctx, streamRes.Stream, t.cfg.GroupName, entry.ID).Err(); ackErr != nil {
					t.logger.Warn(t.ctx, "xack failed", logging.Error(ackErr))
				}
			}
		}
	}
}

func (t *Transport) ensureGroup(stream string) error {
	err := t.client.XGroupCreateMkStream(t.ctx, stream, t.cfg.GroupName, "0").Err()
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return nil
	}
	// 探测组是否已存在
	type xinfo interface {
		XInfoGroups(ctx context.Context, key string) *redis.XInfoGroupsCmd
	}
	if xi, ok := t.client.(xinfo); ok {
		if groups, gerr := xi.XInfoGroups(t.ctx, stream).Result(); gerr == nil {
			for _, g := range groups {
				if g.Name == t.cfg.GroupName {
					return nil
				}
			}
		}
	}
	return err
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

func (t *Transport) streamName(messageType string) string {
	return t.cfg.StreamPrefix + messageType
}

func encodeMessage(msg messaging.IMessage) (map[string]interface{}, error) {
	payload, err := json.Marshal(msg.GetPayload())
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(msg.GetMetadata())
	if err != nil {
		return nil, err
	}
	ts := msg.GetTimestamp()
	if ts.IsZero() {
		ts = time.Now()
	}
	return map[string]interface{}{
		"id":        msg.GetID(),
		"type":      msg.GetType(),
		"timestamp": ts.UnixNano(),
		"payload":   string(payload),
		"metadata":  string(metadata),
	}, nil
}

func decodeMessage(entry redis.XMessage) (messaging.IMessage, error) {
	id, _ := entry.Values["id"].(string)
	msgType, _ := entry.Values["type"].(string)

	payloadRaw, _ := entry.Values["payload"].(string)
	metadataRaw, _ := entry.Values["metadata"].(string)

	var payload interface{}
	if payloadRaw != "" {
		if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
			return nil, err
		}
	}
	metadata := make(map[string]interface{})
	if metadataRaw != "" {
		if err := json.Unmarshal([]byte(metadataRaw), &metadata); err != nil {
			return nil, err
		}
	}

	ts := time.Now()
	switch v := entry.Values["timestamp"].(type) {
	case int64:
		ts = time.Unix(0, v)
	case string:
		if ns, err := parseNanoString(v); err == nil {
			ts = time.Unix(0, ns)
		}
	}

	if id == "" {
		id = entry.ID
	}

	return &messaging.Message{
		ID:        id,
		Type:      msgType,
		Timestamp: ts,
		Payload:   payload,
		Metadata:  metadata,
	}, nil
}

func parseNanoString(v string) (int64, error) {
	return strconv.ParseInt(v, 10, 64)
}
