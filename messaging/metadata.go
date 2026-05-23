package messaging

import (
	"encoding/json"
	"fmt"
	"sync"

	gerrors "gochen/errors"
)

// Metadata 表示消息的元数据（并发安全）。
//
// 设计目标：
//   - 对外提供简单的 Get/Set API，避免直接暴露 map 造成并发读写 panic；
//   - 序列化/反序列化时表现为普通 JSON 对象；
//   - 零值可用（无需显式初始化）。
type Metadata struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMetadata 创建一个新的元数据容器。
func NewMetadata() *Metadata {
	return &Metadata{data: make(map[string]string)}
}

// ensure 确保条件成立。
func (m *Metadata) ensure() {
	if m.data == nil {
		m.data = make(map[string]string)
	}
}

// Get 获取元数据键值。
func (m *Metadata) Get(key string) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.data == nil {
		return "", false
	}
	v, ok := m.data[key]
	return v, ok
}

func (m *Metadata) GetString(key string) (string, bool) { return m.Get(key) }

// Set 设置元数据键值（并发安全）。
func (m *Metadata) Set(key, value string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensure()
	m.data[key] = value
}

// Delete 删除指定元数据键。
func (m *Metadata) Delete(key string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return
	}
	delete(m.data, key)
}

// MapCopy 返回一个浅拷贝的 map（用于序列化/日志/调试）。
func (m *Metadata) MapCopy() map[string]string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.data) == 0 {
		return map[string]string{}
	}
	cp := make(map[string]string, len(m.data))
	for k, v := range m.data {
		cp[k] = v
	}
	return cp
}

// MarshalJSON 将元数据序列化为 JSON 对象。
func (m *Metadata) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return json.Marshal(m.MapCopy())
}

// UnmarshalJSON 从 JSON 对象反序列化元数据。
func (m *Metadata) UnmarshalJSON(b []byte) error {
	if m == nil {
		return nil
	}
	if string(b) == "null" {
		m.mu.Lock()
		m.data = nil
		m.mu.Unlock()
		return nil
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return gerrors.Wrap(err, gerrors.InvalidInput, "invalid metadata json")
	}

	data := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return gerrors.NewCode(gerrors.InvalidInput, "metadata value must be string").
				WithContext("key", k).
				WithContext("type", fmt.Sprintf("%T", v))
		}
		data[k] = s
	}

	m.mu.Lock()
	m.data = data
	m.mu.Unlock()
	return nil
}
