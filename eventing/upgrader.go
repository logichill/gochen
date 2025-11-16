package eventing

import (
	"fmt"
	"sort"
	"sync"
)

// EventUpgrader 定义事件模式升级器接口
type EventUpgrader interface {
	// FromVersion 源模式版本
	FromVersion() int
	// ToVersion 目标模式版本
	ToVersion() int
	// Upgrade 将旧版本事件数据升级为新版本
	Upgrade(data map[string]interface{}) (map[string]interface{}, error)
}

type upgraderRegistry struct {
	upgraders map[string][]EventUpgrader
	mutex     sync.RWMutex
}

var globalUpgraderRegistry = &upgraderRegistry{
	upgraders: make(map[string][]EventUpgrader),
}

// RegisterEventUpgrader 为指定事件类型注册升级器
func RegisterEventUpgrader(eventType string, upgrader EventUpgrader) error {
	if eventType == "" {
		return fmt.Errorf("event type cannot be empty")
	}
	if upgrader == nil {
		return fmt.Errorf("event upgrader cannot be nil for type %s", eventType)
	}
	if upgrader.FromVersion() <= 0 || upgrader.ToVersion() <= 0 {
		return fmt.Errorf("event upgrader versions must be greater than 0 for type %s", eventType)
	}
	if upgrader.ToVersion() <= upgrader.FromVersion() {
		return fmt.Errorf("event upgrader to-version must be greater than from-version for type %s", eventType)
	}

	globalUpgraderRegistry.mutex.Lock()
	defer globalUpgraderRegistry.mutex.Unlock()

	list := globalUpgraderRegistry.upgraders[eventType]
	for _, existing := range list {
		if existing.FromVersion() == upgrader.FromVersion() {
			return fmt.Errorf("event upgrader for type %s from version %d already registered", eventType, upgrader.FromVersion())
		}
	}

	list = append(list, upgrader)
	sort.Slice(list, func(i, j int) bool {
		return list[i].FromVersion() < list[j].FromVersion()
	})
	globalUpgraderRegistry.upgraders[eventType] = list
	return nil
}

// MustRegisterEventUpgrader 注册事件升级器（失败 panic）
func MustRegisterEventUpgrader(eventType string, upgrader EventUpgrader) {
	if err := RegisterEventUpgrader(eventType, upgrader); err != nil {
		panic(err)
	}
}

// UpgradeEventData 根据注册的升级器升级事件数据，返回升级后的数据与最终版本
func UpgradeEventData(eventType string, currentVersion int, data map[string]interface{}) (map[string]interface{}, int, error) {
	targetVersion := GetEventSchemaVersion(eventType)
	if targetVersion <= 0 {
		targetVersion = 1
	}
	if currentVersion <= 0 {
		currentVersion = 1
	}
	if currentVersion >= targetVersion {
		return cloneMap(data), currentVersion, nil
	}

	globalUpgraderRegistry.mutex.RLock()
	upgraders := append([]EventUpgrader(nil), globalUpgraderRegistry.upgraders[eventType]...)
	globalUpgraderRegistry.mutex.RUnlock()

	if len(upgraders) == 0 {
		return nil, currentVersion, fmt.Errorf("no event upgrader registered for type %s", eventType)
	}

	result := cloneMap(data)
	version := currentVersion

	for version < targetVersion {
		nextUpgrader := findNextUpgrader(upgraders, version)
		if nextUpgrader == nil {
			return nil, version, fmt.Errorf("cannot upgrade event %s from version %d to %d: missing upgrader", eventType, version, targetVersion)
		}
		upgraded, err := nextUpgrader.Upgrade(result)
		if err != nil {
			return nil, version, fmt.Errorf("upgrade event %s from version %d failed: %w", eventType, version, err)
		}
		result = cloneMap(upgraded)
		version = nextUpgrader.ToVersion()
	}

	return result, version, nil
}

func findNextUpgrader(upgraders []EventUpgrader, fromVersion int) EventUpgrader {
	for _, upgrader := range upgraders {
		if upgrader.FromVersion() == fromVersion {
			return upgrader
		}
	}
	return nil
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
