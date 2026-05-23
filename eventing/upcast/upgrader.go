package upcast

import (
	"sort"
	"sync"

	"gochen/errors"
	"gochen/eventing/registry"
)

// UpgraderRegistry 是可注入的事件升级器注册表。
//
// 说明：
// - 该类型用于替代包级全局注册表，减少测试/多模块场景的顺序依赖；
// - 组合根推荐显式创建实例并注入到需要做 payload upgrade 的组件中。
type UpgraderRegistry struct {
	inner *upgraderRegistry
}

// IEventUpgrader 定义事件模式升级器接口。
type IEventUpgrader interface {
	// FromVersion 源模式版本
	FromVersion() int
	// ToVersion 目标模式版本
	ToVersion() int
	// Upgrade 将旧版本事件数据升级为新版本
	Upgrade(data map[string]any) (map[string]any, error)
}

type upgraderRegistry struct {
	upgraders map[string][]IEventUpgrader
	mutex     sync.RWMutex
}

// NewUpgraderRegistry 创建Upgrader注册表。
func NewUpgraderRegistry() *UpgraderRegistry {
	return &UpgraderRegistry{
		inner: &upgraderRegistry{upgraders: make(map[string][]IEventUpgrader)},
	}
}

// Reset 重置注册表（测试隔离/启动初始化用）。
func (r *UpgraderRegistry) Reset() {
	if r == nil || r.inner == nil {
		return
	}
	r.inner.mutex.Lock()
	defer r.inner.mutex.Unlock()
	r.inner.upgraders = make(map[string][]IEventUpgrader)
}

// Register 注册事件升级器。
//
// 说明：
// - Register 为指定事件类型注册升级器。
func (r *UpgraderRegistry) Register(eventType string, upgrader IEventUpgrader) error {
	if eventType == "" {
		return errors.NewCode(errors.InvalidInput, "event type cannot be empty")
	}
	if upgrader == nil {
		return errors.NewCode(errors.InvalidInput, "event upgrader cannot be nil").WithContext("event_type", eventType)
	}
	if upgrader.FromVersion() <= 0 || upgrader.ToVersion() <= 0 {
		return errors.NewCode(errors.InvalidInput, "event upgrader versions must be greater than 0").WithContext("event_type", eventType)
	}
	if upgrader.ToVersion() <= upgrader.FromVersion() {
		return errors.NewCode(errors.InvalidInput, "event upgrader to-version must be greater than from-version").WithContext("event_type", eventType)
	}

	if r == nil || r.inner == nil {
		return errors.NewCode(errors.InvalidInput, "upgrader registry cannot be nil").WithContext("event_type", eventType)
	}
	r.inner.mutex.Lock()
	defer r.inner.mutex.Unlock()

	list := r.inner.upgraders[eventType]
	for _, existing := range list {
		if existing.FromVersion() == upgrader.FromVersion() {
			return errors.NewCode(errors.Conflict, "event upgrader already registered").
				WithContext("event_type", eventType).
				WithContext("from_version", upgrader.FromVersion())
		}
	}

	list = append(list, upgrader)
	sort.Slice(list, func(i, j int) bool {
		return list[i].FromVersion() < list[j].FromVersion()
	})
	r.inner.upgraders[eventType] = list
	return nil
}

// UpgradeEventData 根据注册的升级器升级事件数据，返回升级后的数据与最终版本（显式注入 registry/upgraders）。
// 参数：
// - reg：事件 registry（用于获取目标 schemaVersion）
func UpgradeEventData(
	reg *registry.Registry,
	upgraders *UpgraderRegistry,
	eventType string,
	currentVersion int,
	data map[string]any,
) (map[string]any, int, error) {
	if reg == nil {
		return nil, currentVersion, errors.NewCode(errors.InvalidInput, "event registry cannot be nil").WithContext("event_type", eventType)
	}
	if upgraders == nil {
		return nil, currentVersion, errors.NewCode(errors.InvalidInput, "event upgrader registry cannot be nil").WithContext("event_type", eventType)
	}
	if !reg.HasEvent(eventType) {
		return nil, currentVersion, errors.NewCode(errors.NotFound, "unknown event type").WithContext("event_type", eventType)
	}

	targetVersion := reg.EventSchemaVersion(eventType)
	if currentVersion <= 0 {
		currentVersion = 1
	}
	if currentVersion >= targetVersion {
		return cloneMap(data), currentVersion, nil
	}

	list := upgraders.snapshot(eventType)

	if len(list) == 0 {
		return nil, currentVersion, errors.NewCode(errors.NotFound, "no event upgrader registered").WithContext("event_type", eventType)
	}

	result := cloneMap(data)
	version := currentVersion

	for version < targetVersion {
		nextUpgrader := findNextUpgrader(list, version)
		if nextUpgrader == nil {
			return nil, version, errors.NewCode(errors.NotFound, "missing event upgrader").
				WithContext("event_type", eventType).
				WithContext("from_version", version).
				WithContext("target_version", targetVersion)
		}
		upgraded, err := nextUpgrader.Upgrade(result)
		if err != nil {
			return nil, version, errors.Wrap(err, errors.Internal, "upgrade event failed").
				WithContext("event_type", eventType).
				WithContext("from_version", version).
				WithContext("to_version", nextUpgrader.ToVersion())
		}
		result = cloneMap(upgraded)
		version = nextUpgrader.ToVersion()
	}

	return result, version, nil
}

func (r *UpgraderRegistry) snapshot(eventType string) []IEventUpgrader {
	if r == nil || r.inner == nil {
		return nil
	}
	r.inner.mutex.RLock()
	list := append([]IEventUpgrader(nil), r.inner.upgraders[eventType]...)
	r.inner.mutex.RUnlock()
	return list
}

// findNextUpgrader 执行对应操作。
func findNextUpgrader(upgraders []IEventUpgrader, fromVersion int) IEventUpgrader {
	for _, upgrader := range upgraders {
		if upgrader.FromVersion() == fromVersion {
			return upgrader
		}
	}
	return nil
}

// cloneMap 复制Map。
func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneValue(v)
	}
	return dst
}

// cloneSlice 复制Slice。
func cloneSlice(src []any) []any {
	if src == nil {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		dst[i] = cloneValue(v)
	}
	return dst
}

// cloneValue 复制值。
func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	default:
		return v
	}
}
