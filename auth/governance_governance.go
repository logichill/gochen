// Package governance 提供 L4 企业治理外壳：策略版本留痕、决策审计与监控。
//
// 设计要点：
//   - 治理能力实现为 **Authorizer Decorator**（包装 IAuthorizer），
//     而不是包装 Application——不使用时完全不参与编译；
//   - 固化调用链 Metrics(Log(Snapshot(base)))，包装顺序由本包决定，
//     不允许业务层自行拼接（§4.3 风险控制）。
package auth

// ConsistencyMode 表达留痕中的策略新鲜度声明，实际保证由策略实现负责。
// Wrap 不依据该值选择读取策略或绑定评估版本。
type ConsistencyMode string

const (
	// ConsistencyModeUnspecified 表示未声明策略新鲜度要求。
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
