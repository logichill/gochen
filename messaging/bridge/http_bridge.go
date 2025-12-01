package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"gochen/logging"
	"gochen/messaging"
	"gochen/messaging/command"
)

func bridgeLogger() logging.ILogger {
	return logging.GetLogger().WithFields(
		logging.String("component", "bridge.http"),
	)
}

// HTTPBridgeConfig HTTP 桥接配置
type HTTPBridgeConfig struct {
	// ListenAddr 监听地址
	ListenAddr string

	// ReadTimeout 读取超时
	ReadTimeout time.Duration

	// WriteTimeout 写入超时
	WriteTimeout time.Duration

	// IdleTimeout 空闲超时
	IdleTimeout time.Duration

	// MaxBodySize 最大请求体大小
	MaxBodySize int64

	// ClientTimeout 客户端超时
	ClientTimeout time.Duration
}

// DefaultHTTPBridgeConfig 默认配置
func DefaultHTTPBridgeConfig() *HTTPBridgeConfig {
	return &HTTPBridgeConfig{
		ListenAddr:    ":8080",
		ReadTimeout:   30 * time.Second,
		WriteTimeout:  30 * time.Second,
		IdleTimeout:   60 * time.Second,
		MaxBodySize:   10 * 1024 * 1024, // 10MB
		ClientTimeout: 30 * time.Second,
	}
}

// HTTPBridge HTTP 桥接实现
//
// 使用 HTTP 协议进行跨服务通信。
//
// 特性：
//   - 标准 HTTP/HTTPS 协议
//   - JSON 序列化
//   - 超时控制
//   - 错误处理
type HTTPBridge struct {
	config     *HTTPBridgeConfig
	serializer ISerializer
	server     *http.Server
	client     *http.Client
	mux        *http.ServeMux

	// 命令处理器
	commandHandlers map[string]func(ctx context.Context, cmd *command.Command) error
	commandMutex    sync.RWMutex

	// 事件处理器
	eventHandlers map[string]messaging.IMessageHandler
	eventMutex    sync.RWMutex

	running bool
	mutex   sync.RWMutex
}

// NewHTTPBridge 创建 HTTP 桥接
//
// 参数：
//   - config: 配置（nil 使用默认配置）
//
// 返回：
//   - *HTTPBridge: 桥接实例
func NewHTTPBridge(config *HTTPBridgeConfig) *HTTPBridge {
	if config == nil {
		config = DefaultHTTPBridgeConfig()
	}

	mux := http.NewServeMux()

	bridge := &HTTPBridge{
		config:          config,
		serializer:      NewJSONSerializer(),
		mux:             mux,
		commandHandlers: make(map[string]func(ctx context.Context, cmd *command.Command) error),
		eventHandlers:   make(map[string]messaging.IMessageHandler),
		client: &http.Client{
			Timeout: config.ClientTimeout,
		},
	}

	// 注册路由
	mux.HandleFunc("/commands/", bridge.handleCommand)
	mux.HandleFunc("/events/", bridge.handleEvent)
	mux.HandleFunc("/health", bridge.handleHealth)

	bridge.server = &http.Server{
		Addr:         config.ListenAddr,
		Handler:      mux,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		IdleTimeout:  config.IdleTimeout,
	}

	return bridge
}

// SendCommand 发送命令到远程服务
func (b *HTTPBridge) SendCommand(ctx context.Context, serviceURL string, cmd *command.Command) error {
	if cmd == nil {
		return ErrInvalidMessage
	}

	// 序列化命令
	data, err := b.serializer.SerializeCommand(cmd)
	if err != nil {
		return err
	}

	// 构建 URL
	url := fmt.Sprintf("%s/commands/%s", serviceURL, cmd.GetMetadata()["command_type"])

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return errors.Join(ErrRemoteCallFailed, err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := b.client.Do(req)
	if err != nil {
		return errors.Join(ErrRemoteCallFailed, err)
	}
	defer resp.Body.Close()

	// 检查响应
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status=%d, body=%s", ErrRemoteCallFailed, resp.StatusCode, string(body))
	}

	bridgeLogger().Debug(ctx, "命令发送成功",
		logging.String("url", url),
		logging.String("command_id", cmd.GetID()))

	return nil
}

// SendEvent 发送事件到远程服务
func (b *HTTPBridge) SendEvent(ctx context.Context, serviceURL string, event messaging.IMessage) error {
	if event == nil {
		return ErrInvalidMessage
	}

	// 序列化事件
	data, err := b.serializer.SerializeEvent(event)
	if err != nil {
		return err
	}

	// 构建 URL
	url := fmt.Sprintf("%s/events/%s", serviceURL, event.GetType())

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return errors.Join(ErrRemoteCallFailed, err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := b.client.Do(req)
	if err != nil {
		return errors.Join(ErrRemoteCallFailed, err)
	}
	defer resp.Body.Close()

	// 检查响应
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status=%d, body=%s", ErrRemoteCallFailed, resp.StatusCode, string(body))
	}

	bridgeLogger().Debug(ctx, "事件发送成功",
		logging.String("url", url),
		logging.String("event_id", event.GetID()))

	return nil
}

// RegisterCommandHandler 注册命令处理器
func (b *HTTPBridge) RegisterCommandHandler(commandType string, handler func(ctx context.Context, cmd *command.Command) error) error {
	if handler == nil {
		return ErrInvalidMessage
	}

	b.commandMutex.Lock()
	defer b.commandMutex.Unlock()

	b.commandHandlers[commandType] = handler

	bridgeLogger().Info(context.Background(), "注册命令处理器",
		logging.String("command_type", commandType))

	return nil
}

// RegisterEventHandler 注册事件处理器
func (b *HTTPBridge) RegisterEventHandler(eventType string, handler messaging.IMessageHandler) error {
	if handler == nil {
		return ErrInvalidMessage
	}

	b.eventMutex.Lock()
	defer b.eventMutex.Unlock()

	b.eventHandlers[eventType] = handler

	bridgeLogger().Info(context.Background(), "注册事件处理器",
		logging.String("event_type", eventType))

	return nil
}

// handleCommand 处理命令请求
func (b *HTTPBridge) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 提取命令类型
	commandType := r.URL.Path[len("/commands/"):]
	if commandType == "" {
		http.Error(w, "Command type is required", http.StatusBadRequest)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(io.LimitReader(r.Body, b.config.MaxBodySize))
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// 反序列化命令
	cmd, err := b.serializer.DeserializeCommand(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to deserialize command: %v", err), http.StatusBadRequest)
		return
	}

	// 查找处理器
	b.commandMutex.RLock()
	handler, exists := b.commandHandlers[commandType]
	b.commandMutex.RUnlock()

	if !exists {
		http.Error(w, fmt.Sprintf("Handler not found for command type: %s", commandType), http.StatusNotFound)
		return
	}

	// 执行处理器
	if err := handler(r.Context(), cmd); err != nil {
		bridgeLogger().Error(r.Context(), "命令处理失败", logging.Error(err),
			logging.String("command_type", commandType))
		http.Error(w, fmt.Sprintf("Command handler failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleEvent 处理事件请求
func (b *HTTPBridge) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 提取事件类型
	eventType := r.URL.Path[len("/events/"):]
	if eventType == "" {
		http.Error(w, "Event type is required", http.StatusBadRequest)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(io.LimitReader(r.Body, b.config.MaxBodySize))
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// 反序列化事件
	event, err := b.serializer.DeserializeEvent(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to deserialize event: %v", err), http.StatusBadRequest)
		return
	}

	// 查找处理器
	b.eventMutex.RLock()
	handler, exists := b.eventHandlers[eventType]
	b.eventMutex.RUnlock()

	if !exists {
		http.Error(w, fmt.Sprintf("Handler not found for event type: %s", eventType), http.StatusNotFound)
		return
	}

	// 执行处理器
	if err := handler.Handle(r.Context(), event); err != nil {
		bridgeLogger().Error(r.Context(), "事件处理失败", logging.Error(err),
			logging.String("event_type", eventType))
		http.Error(w, fmt.Sprintf("Event handler failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleHealth 健康检查
func (b *HTTPBridge) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// Start 启动服务端
func (b *HTTPBridge) Start() error {
	b.mutex.Lock()
	if b.running {
		b.mutex.Unlock()
		return nil
	}
	b.running = true
	b.mutex.Unlock()

	bridgeLogger().Info(context.Background(), "启动 HTTP Bridge",
		logging.String("addr", b.config.ListenAddr))

	// 在 goroutine 中启动服务器
	go func() {
		if err := b.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			bridgeLogger().Error(context.Background(), "HTTP Bridge 启动失败", logging.Error(err))
		}
	}()

	return nil
}

// Stop 停止服务端
func (b *HTTPBridge) Stop() error {
	b.mutex.Lock()
	if !b.running {
		b.mutex.Unlock()
		return nil
	}
	b.running = false
	b.mutex.Unlock()

	bridgeLogger().Info(context.Background(), "停止 HTTP Bridge")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return b.server.Shutdown(ctx)
}

// Ensure HTTPBridge implements IRemoteBridge
var _ IRemoteBridge = (*HTTPBridge)(nil)
