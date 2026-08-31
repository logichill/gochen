// Package propagation 提供跨进程上下文传播使用的最小载体抽象。
package propagation

// ICarrier 表示可读写的字符串传播载体。
type ICarrier interface {
	Get(key string) (string, bool)
	Set(key, value string)
}

// IEnumerableCarrier 是需要枚举键的传播载体能力（例如 tracing adapter）。
type IEnumerableCarrier interface {
	ICarrier
	Keys() []string
}

// MapCarrier 基于 map 承载传播字段。
type MapCarrier map[string]string

// NewMapCarrier 创建可写的 map 载体。
func NewMapCarrier() MapCarrier { return make(MapCarrier) }

// Get 返回 key 对应的值；不存在时 ok 为 false。
func (c MapCarrier) Get(key string) (string, bool) {
	value, ok := c[key]
	return value, ok
}

// Set 写入传播字段；nil map 不可写，调用方应通过 NewMapCarrier 或 map literal 创建。
func (c MapCarrier) Set(key, value string) {
	if c == nil {
		panic("propagation.MapCarrier.Set called on nil map; use NewMapCarrier")
	}
	c[key] = value
}

// Keys 返回当前载体中的全部键。
func (c MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

var _ IEnumerableCarrier = MapCarrier(nil)
