package workflow

import "time"

// ID 表示流程实例标识。
type ID string

// InstanceStatus 表示工作流实例状态。
type InstanceStatus string

const (
	// InstanceStatusPending 表示实例已创建但尚未启动。
	InstanceStatusPending InstanceStatus = "pending"

	// InstanceStatusRunning 表示实例正在推进中。
	InstanceStatusRunning InstanceStatus = "running"

	// InstanceStatusSuspended 表示实例已被显式挂起，等待恢复。
	InstanceStatusSuspended InstanceStatus = "suspended"

	// InstanceStatusCompleted 表示实例已全部推进完成。
	InstanceStatusCompleted InstanceStatus = "completed"

	// InstanceStatusFailed 表示实例已执行失败。
	InstanceStatusFailed InstanceStatus = "failed"

	// InstanceStatusTerminated 表示实例已被显式终止。
	InstanceStatusTerminated InstanceStatus = "terminated"
)

// HistoryEntry 记录一次实例状态变化轨迹。
type HistoryEntry struct {
	// Action 表示本次状态变化类型，例如 create、start、advance、choice、branch、
	// join_wait、join_ready、reject、suspend、resume、terminate、timeout、complete。
	Action string `json:"action"`

	// NodeID 表示本次变化涉及的目标节点。
	NodeID string `json:"node_id,omitempty"`

	// FromNodeID 表示状态变化来自哪个前驱节点。
	FromNodeID string `json:"from_node_id,omitempty"`

	// Reason 记录本次操作附带的说明或原因（例如挂起/终止原因）。
	Reason string `json:"reason,omitempty"`

	// At 表示状态变化发生时间。
	At time.Time `json:"at"`
}

// PendingJoin 表示一个多入边节点当前已到达的前驱状态。
type PendingJoin struct {
	// NodeID 是等待激活的汇聚节点。
	NodeID string `json:"node_id"`

	// ExpectedCount 是该汇聚节点需要等待的前驱数量。
	ExpectedCount int `json:"expected_count"`

	// ArrivedFrom 记录已经到达的前驱节点 ID。
	ArrivedFrom []string `json:"arrived_from"`
}

// State 表示工作流实例状态。
type State struct {
	// ID 是流程实例标识。
	ID ID `json:"id"`

	// DefinitionID 指向创建该实例时使用的流程定义标识。
	DefinitionID string `json:"definition_id"`

	// DefinitionVersion 固化创建该实例时绑定的流程定义版本号。
	DefinitionVersion uint32 `json:"definition_version"`

	// Version 是实例状态版本号，用于乐观并发控制。
	Version uint64 `json:"version"`

	// Status 表示实例当前生命周期状态。
	Status InstanceStatus `json:"status"`

	// ActiveNodeIDs 是当前可推进的活动节点集合。
	ActiveNodeIDs []string `json:"active_node_ids"`

	// ActiveNodeStartedAt 记录每个活动节点的激活时刻，用于超时检测。
	ActiveNodeStartedAt map[string]time.Time `json:"active_node_started_at,omitempty"`

	// PendingJoins 是已经部分到达、但尚未满足全部入边的汇聚节点。
	PendingJoins []PendingJoin `json:"pending_joins"`

	// CompletedNodeIDs 按完成顺序记录已推进完成的节点。
	CompletedNodeIDs []string `json:"completed_node_ids"`

	// History 记录实例状态变化轨迹。
	History []HistoryEntry `json:"history"`

	// Data 保存调用方附加的实例业务数据。
	Data map[string]any `json:"data,omitempty"`

	// StartedAt 表示实例启动时间。
	StartedAt time.Time `json:"started_at,omitempty"`

	// CompletedAt 表示实例完成或终止时间。
	CompletedAt time.Time `json:"completed_at,omitempty"`

	// CreatedAt 表示实例创建时间。
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 表示实例最近更新时间。
	UpdatedAt time.Time `json:"updated_at"`
}
