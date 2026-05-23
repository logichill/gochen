package workflow

import "time"

const (
	// NodeKindTask 表示普通任务节点；多个入边会按“任一到达即可激活”的 exclusive merge 处理。
	NodeKindTask NodeKind = "task"
	// NodeKindBranch 表示显式选择或并行分支节点。
	NodeKindBranch NodeKind = "branch"
	// NodeKindJoin 表示等待全部入边到达的汇聚节点。
	NodeKindJoin NodeKind = "join"
)

// NodeKind 描述节点在运行时处理多入边时的语义。
//
// 空值保持历史兼容：核心 runtime 会继续按“多个入边即 join-all”推断。
type NodeKind string

// Definition 表示一个可执行工作流定义。
type Definition struct {
	// ID 是流程定义的稳定标识。
	ID string

	// Name 是面向用户展示的定义名称。
	Name string

	// Nodes 是流程 DAG 中的全部节点集合。
	Nodes []Node

	// StartNodeID 指向实例启动后第一个活动节点。
	StartNodeID string

	// CreatedAt 记录定义首次保存时间。
	CreatedAt time.Time

	// UpdatedAt 记录定义最近保存时间。
	UpdatedAt time.Time
}

// Node 表示工作流图中的一个节点。
type Node struct {
	// ID 是节点在定义内的唯一标识。
	ID string

	// Name 是面向用户展示的节点名称。
	Name string

	// Kind 显式声明节点语义；为空时保持旧版基于图结构推断的行为。
	Kind NodeKind

	// Next 是当前节点完成后可到达的后继节点 ID 列表。
	// 一个后继表示线性推进，多个后继表示分支；多个前驱指向同一节点时表示汇聚。
	Next []string
}
