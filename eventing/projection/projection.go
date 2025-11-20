// Package projection 提供投影管理功能
package projection

import (
    "context"
    "fmt"
    "sync"
    "time"

    "gochen/eventing"
    "gochen/eventing/store"
    "gochen/logging"
)

// Projector 投影器接口
type Projector interface {
    GetName() string
    Handle(ctx context.Context, event eventing.Event) error
    GetSupportedEventTypes() []string
    Rebuild(ctx context.Context, events []eventing.Event) error
}

// Status 投影状态
type Status struct {
    Name            string    `json:"name"`
    LastEventID     string    `json:"last_event_id"`
    LastEventTime   time.Time `json:"last_event_time"`
    ProcessedEvents int64     `json:"processed_events"`
    FailedEvents    int64     `json:"failed_events"`
    Status          string    `json:"status"`
    LastError       string    `json:"last_error,omitempty"`
    UpdatedAt       time.Time `json:"updated_at"`
}

// Manager 投影管理器
//
// Deprecated: 请使用功能更完整、支持检查点和事件总线集成的 ProjectionManager。
type Manager struct {
    projectors map[string]Projector
    statuses   map[string]*Status
    logger     logging.Logger
    mutex      sync.RWMutex
}

// NewManager 创建投影管理器
//
// Deprecated: 请改用 NewProjectionManager 或 NewProjectionManagerWithConfig。
func NewManager(eventStore store.IEventStore, config interface{}) *Manager {
    return &Manager{
        projectors: make(map[string]Projector),
        statuses:   make(map[string]*Status),
        logger:     logging.GetLogger(),
    }
}

// Register 注册投影器
func (m *Manager) Register(projector Projector) error {
    m.mutex.Lock()
    defer m.mutex.Unlock()

    name := projector.GetName()
    if _, exists := m.projectors[name]; exists {
        return fmt.Errorf("projector %s already registered", name)
    }

    m.projectors[name] = projector
    m.statuses[name] = &Status{
        Name:      name,
        Status:    "stopped",
        UpdatedAt: time.Now(),
    }

    m.logger.Info(context.Background(), "投影器已注册",
        logging.String("projector", name),
    )

    return nil
}

// Unregister 取消注册投影器
func (m *Manager) Unregister(name string) error {
    m.mutex.Lock()
    defer m.mutex.Unlock()

    if _, exists := m.projectors[name]; !exists {
        return fmt.Errorf("projector %s not found", name)
    }

    delete(m.projectors, name)
    delete(m.statuses, name)

    m.logger.Info(context.Background(), "投影器已取消注册",
        logging.String("projector", name),
    )

    return nil
}

// Start 启动投影器
func (m *Manager) Start(name string) error {
    m.mutex.Lock()
    defer m.mutex.Unlock()

    status, exists := m.statuses[name]
    if !exists {
        return fmt.Errorf("projector %s not found", name)
    }

    status.Status = "running"
    status.UpdatedAt = time.Now()

    m.logger.Info(context.Background(), "投影器已启动",
        logging.String("projector", name),
    )

    return nil
}

// Stop 停止投影器
func (m *Manager) Stop(name string) error {
    m.mutex.Lock()
    defer m.mutex.Unlock()

    status, exists := m.statuses[name]
    if !exists {
        return fmt.Errorf("projector %s not found", name)
    }

    status.Status = "stopped"
    status.UpdatedAt = time.Now()

    m.logger.Info(context.Background(), "投影器已停止",
        logging.String("projector", name),
    )

    return nil
}

// Rebuild 重建投影
func (m *Manager) Rebuild(ctx context.Context, name string, events []eventing.Event) error {
    m.mutex.RLock()
    projector, exists := m.projectors[name]
    status := m.statuses[name]
    m.mutex.RUnlock()

    if !exists {
        return fmt.Errorf("projector %s not found", name)
    }

    m.logger.Info(ctx, "开始重建投影",
        logging.String("projector", name),
        logging.Int("events", len(events)),
    )

    if err := projector.Rebuild(ctx, events); err != nil {
        return fmt.Errorf("rebuild failed: %w", err)
    }

    m.mutex.Lock()
    status.ProcessedEvents = int64(len(events))
    status.UpdatedAt = time.Now()
    m.mutex.Unlock()

    m.logger.Info(ctx, "投影重建完成",
        logging.String("projector", name),
        logging.Int64("processed", status.ProcessedEvents),
    )

    return nil
}

// GetStatus 获取投影状态
func (m *Manager) GetStatus(name string) (*Status, error) {
    m.mutex.RLock()
    defer m.mutex.RUnlock()

    status, exists := m.statuses[name]
    if !exists {
        return nil, fmt.Errorf("projector %s not found", name)
    }

    statusCopy := *status
    return &statusCopy, nil
}

// GetAllStatuses 获取所有投影状态
func (m *Manager) GetAllStatuses() map[string]*Status {
    m.mutex.RLock()
    defer m.mutex.RUnlock()

    result := make(map[string]*Status, len(m.statuses))
    for name, status := range m.statuses {
        statusCopy := *status
        result[name] = &statusCopy
    }
    return result
}

// PublishEvent 发布事件给投影器
func (m *Manager) PublishEvent(ctx context.Context, event eventing.Event) error {
    m.mutex.RLock()
    projectors := make([]Projector, 0, len(m.projectors))
    for _, p := range m.projectors {
        projectors = append(projectors, p)
    }
    m.mutex.RUnlock()

    for _, projector := range projectors {
        m.mutex.RLock()
        status := m.statuses[projector.GetName()]
        m.mutex.RUnlock()

        if status.Status != "running" {
            continue
        }

        // 检查投影器是否支持该事件类型
        supportedTypes := projector.GetSupportedEventTypes()
        supported := false
        for _, t := range supportedTypes {
            if t == event.Type {
                supported = true
                break
            }
        }
        if !supported {
            continue
        }

        if err := projector.Handle(ctx, event); err != nil {
            m.mutex.Lock()
            status.FailedEvents++
            status.LastError = err.Error()
            status.UpdatedAt = time.Now()
            m.mutex.Unlock()

            m.logger.Error(ctx, "投影处理事件失败",
                logging.String("projector", projector.GetName()),
                logging.String("event_type", event.GetType()),
                logging.Error(err),
            )
        } else {
            m.mutex.Lock()
            status.LastEventID = event.GetID()
            status.LastEventTime = event.GetTimestamp()
            status.ProcessedEvents++
            status.UpdatedAt = time.Now()
            m.mutex.Unlock()
        }
    }

    return nil
}
