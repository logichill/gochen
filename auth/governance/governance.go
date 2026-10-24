// Package governance 提供 L4 企业治理外壳：策略快照一致性、决策审计留痕与监控。
//
// 设计要点：
//   - 治理能力实现为 **Authorizer Decorator**（包装 scoped.IAuthorizer），
//     而不是包装 Application——不使用时完全不参与编译；
//   - 固化调用链 Metrics(Log(Snapshot(base)))，包装顺序由本包决定，
//     不允许业务层自行拼接（§4.3 风险控制）。
package governance

// ConsistencyMode 表达授权判定对策略新鲜度的要求。
type ConsistencyMode string

const (
	// ConsistencyModeUnspecified 未显式声明，由框架按默认值处理。
	ConsistencyModeUnspecified ConsistencyMode = ""
	// ConsistencyModeStrong 必须基于当前时刻的实时权限事实判定。
	ConsistencyModeStrong ConsistencyMode = "strong"
	// ConsistencyModeBoundedStaleness 允许在受控范围内使用陈旧快照。
	ConsistencyModeBoundedStaleness ConsistencyMode = "bounded_staleness"
)

// ExecutionMetadata 表达一次授权执行链的追踪元数据。
type ExecutionMetadata struct {
	InitiatorID string
	ActorID     string
	RequestID   string
	DecisionID  string
	EventID     string
	JobID       string
}
