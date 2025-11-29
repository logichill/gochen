package orm

// Capability 表示适配器可选支持的能力标识。
// 设计为最小能力子集，超出能力的调用应由适配器返回明确错误，而非静默降级。
type Capability string

const (
	CapabilityBasicCRUD        Capability = "basic_crud"
	CapabilityQuery            Capability = "query"
	CapabilityPreload          Capability = "preload"
	CapabilityAssociationWrite Capability = "association_write"
	CapabilityBatchWrite       Capability = "batch_write"
	CapabilityTransaction      Capability = "transaction"
	CapabilityOptimisticLock   Capability = "optimistic_lock"
	CapabilityMigration        Capability = "migration"
)

// Capabilities 以集合形式表达适配器支持的能力。
type Capabilities map[Capability]bool

// Supports 判断是否支持指定能力。
func (c Capabilities) Supports(cap Capability) bool {
	if c == nil {
		return false
	}
	return c[cap]
}

// NewCapabilities 便捷构造能力集合。
func NewCapabilities(caps ...Capability) Capabilities {
	set := make(Capabilities, len(caps))
	for _, cap := range caps {
		set[cap] = true
	}
	return set
}
