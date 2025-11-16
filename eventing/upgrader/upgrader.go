// Package upgrader 提供事件升级器，用于事件Schema演化
package upgrader

import (
	"context"
	"fmt"
	"sync"

	"gochen/eventing"
	"gochen/logging"
)

// EventUpgrader 事件升级器接口
type EventUpgrader interface {
	// Upgrade 升级事件
	Upgrade(ctx context.Context, event eventing.Event) (eventing.Event, error)

	// GetFromVersion 获取源版本
	GetFromVersion() int

	// GetToVersion 获取目标版本
	GetToVersion() int

	// GetEventType 获取事件类型
	GetEventType() string
}

// UpgradeChain 升级链
type UpgradeChain struct {
	upgraders map[string][]EventUpgrader // eventType -> upgraders
	logger    logging.Logger
	mutex     sync.RWMutex
}

// NewUpgradeChain 创建升级链
func NewUpgradeChain() *UpgradeChain {
	return &UpgradeChain{
		upgraders: make(map[string][]EventUpgrader),
		logger:    logging.GetLogger(),
	}
}

// Register 注册升级器
func (c *UpgradeChain) Register(upgrader EventUpgrader) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	eventType := upgrader.GetEventType()
	c.upgraders[eventType] = append(c.upgraders[eventType], upgrader)

	c.logger.Info(context.Background(), "注册事件升级器",
		logging.String("event_type", eventType),
		logging.Int("from_version", upgrader.GetFromVersion()),
		logging.Int("to_version", upgrader.GetToVersion()),
	)

	return nil
}

// Upgrade 升级事件
func (c *UpgradeChain) Upgrade(ctx context.Context, event eventing.Event) (eventing.Event, error) {
	c.mutex.RLock()
	upgraders := c.upgraders[event.GetType()]
	c.mutex.RUnlock()

	if len(upgraders) == 0 {
		return event, nil // 无需升级
	}

	currentEvent := event
	currentVersion := event.GetSchemaVersion()

	// 按版本顺序升级
	for {
		upgraded := false
		for _, upgrader := range upgraders {
			if upgrader.GetFromVersion() == currentVersion {
				newEvent, err := upgrader.Upgrade(ctx, currentEvent)
				if err != nil {
					var empty eventing.Event
					return empty, fmt.Errorf("upgrade failed from v%d to v%d: %w",
						upgrader.GetFromVersion(), upgrader.GetToVersion(), err)
				}
				currentEvent = newEvent
				currentVersion = upgrader.GetToVersion()
				upgraded = true
				break
			}
		}

		if !upgraded {
			break // 没有更多升级器
		}
	}

	if currentVersion != event.GetSchemaVersion() {
		c.logger.Info(ctx, "事件已升级",
			logging.String("event_type", event.GetType()),
			logging.Int("from_version", event.GetSchemaVersion()),
			logging.Int("to_version", currentVersion),
		)
	}

	return currentEvent, nil
}

// UpgradeAll 批量升级事件
func (c *UpgradeChain) UpgradeAll(ctx context.Context, events []eventing.Event) ([]eventing.Event, error) {
	result := make([]eventing.Event, 0, len(events))

	for _, event := range events {
		upgraded, err := c.Upgrade(ctx, event)
		if err != nil {
			return nil, fmt.Errorf("upgrade event %s failed: %w", event.GetID(), err)
		}
		result = append(result, upgraded)
	}

	return result, nil
}

// 全局升级链
var globalChain = NewUpgradeChain()

// RegisterGlobal 注册到全局升级链
func RegisterGlobal(upgrader EventUpgrader) error {
	return globalChain.Register(upgrader)
}

// UpgradeGlobal 使用全局升级链升级
func UpgradeGlobal(ctx context.Context, event eventing.Event) (eventing.Event, error) {
	return globalChain.Upgrade(ctx, event)
}
