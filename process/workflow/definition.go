package workflow

import "time"

const (
	// NodeKindTask 表示普通任务节点；多个入边会按“任一到达即可激活”的 exclusive merge 处理。
	NodeKindTask NodeKind = "task"
	// NodeKindBranch 表示显式选择或条件/并行分支节点。
	NodeKindBranch NodeKind = "branch"
	// NodeKindJoin 表示等待全部入边到达的汇聚节点。
	NodeKindJoin NodeKind = "join"
)

// NodeKind 描述节点在运行时处理多入边时的语义。
// 最多一条入边的节点可留空，等价于 NodeKindTask；多入边节点必须显式声明 Kind。
type NodeKind string

// Edge 描述节点的一条出边及可选流转条件。
//
// 三类出边的运行期语义互不相同：
//   - 无条件边（Condition 为空、Default 为 false）：总是激活，多条无条件边表示并行分支。
//   - 条件边（Condition 非空）：条件为真时激活。
//   - 默认边（Default 为 true）：仅当同一节点上没有任何条件边命中时才激活，即 else 分支。
type Edge struct {
	// Target 是出边指向的目标节点 ID。
	Target string `json:"target"`

	// Condition 是可选的条件表达式（例如 "data.amount > 1000" 或 "approved == true"）。
	// 为空且 Default 为 false 时表示无条件走该分支。
	Condition string `json:"condition,omitempty"`

	// Default 声明该出边为默认（else）分支；同一节点最多一条，且不得携带 Condition。
	Default bool `json:"default,omitempty"`
}

// NewEdge 创建一条无条件出边。
func NewEdge(target string) Edge {
	return Edge{Target: target}
}

// NewConditionalEdge 创建一条带条件的出边。
func NewConditionalEdge(target, condition string) Edge {
	return Edge{Target: target, Condition: condition}
}

// NewDefaultEdge 创建一条默认（else）出边，用于兜住所有条件边都不命中的情况。
func NewDefaultEdge(target string) Edge {
	return Edge{Target: target, Default: true}
}

// NewEdges 便捷批量创建无条件出边列表。
func NewEdges(targets ...string) []Edge {
	edges := make([]Edge, len(targets))
	for i, t := range targets {
		edges[i] = Edge{Target: t}
	}
	return edges
}

// Definition 表示一个可执行工作流定义。
type Definition struct {
	// ID 是流程定义的稳定标识。
	ID string `json:"id"`

	// Version 是流程定义的递增版本号（从 1 开始）。
	// 实例在创建时固化绑定该版本号，防止热更新破坏运行中实例。
	Version uint32 `json:"version"`

	// Name 是面向用户展示的定义名称。
	Name string `json:"name"`

	// Nodes 是流程 DAG 中的全部节点集合。
	Nodes []Node `json:"nodes"`

	// StartNodeID 指向实例启动后第一个活动节点。
	StartNodeID string `json:"start_node_id"`

	// CreatedAt 记录定义首次保存时间。
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 记录定义最近保存时间。
	UpdatedAt time.Time `json:"updated_at"`
}

// Node 表示工作流图中的一个节点。
type Node struct {
	// ID 是节点在定义内的唯一标识。
	ID string `json:"id"`

	// Name 是面向用户展示的节点名称。
	Name string `json:"name"`

	// Kind 声明节点语义；多入边节点必须显式设置。
	Kind NodeKind `json:"kind"`

	// Edges 是当前节点完成后可到达的后继出边列表（包含目标节点与可选条件）。
	Edges []Edge `json:"edges"`

	// RejectTo 是当前节点允许驳回回退到的目标节点 ID 列表。
	// 目标必须是当前节点的严格支配祖先，避免回退后留下不可恢复的分支状态。
	RejectTo []string `json:"reject_to"`

	// Timeout 是当前节点允许停留的最大超时时间；0 表示不设超时。
	Timeout time.Duration `json:"timeout,omitempty"`
}

// NextNodes 提取当前节点的所有后继目标节点 ID。
func (n Node) NextNodes() []string {
	if len(n.Edges) == 0 {
		return nil
	}
	targets := make([]string, len(n.Edges))
	for i, edge := range n.Edges {
		targets[i] = edge.Target
	}
	return targets
}
