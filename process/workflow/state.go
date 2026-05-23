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

	// InstanceStatusCompleted 表示实例已完成。
	InstanceStatusCompleted InstanceStatus = "completed"

	// InstanceStatusFailed 表示实例已失败。
	InstanceStatusFailed InstanceStatus = "failed"
)

// HistoryEntry 记录一次实例状态变化。
type HistoryEntry struct {
	// Action 表示本次状态变化类型，例如 create、start、advance、choice、branch、
	// join_wait、join_ready、complete。
	Action string

	// NodeID 表示本次变化涉及的目标节点。
	NodeID string

	// FromNodeID 表示状态变化来自哪个前驱节点。
	FromNodeID string

	// At 表示状态变化发生时间。
	At time.Time
}

// PendingJoin 表示一个多入边节点当前已到达的前驱状态。
type PendingJoin struct {
	// NodeID 是等待激活的汇聚节点。
	NodeID string

	// ExpectedCount 是该汇聚节点需要等待的前驱数量。
	ExpectedCount int

	// ArrivedFrom 记录已经到达的前驱节点 ID。
	ArrivedFrom []string
}

// State 表示工作流实例状态。
type State struct {
	// ID 是流程实例标识。
	ID ID

	// DefinitionID 指向创建该实例时使用的流程定义。
	DefinitionID string

	// Version 是实例状态版本号，用于乐观并发控制。
	Version uint64

	// Status 表示实例当前生命周期状态。
	Status InstanceStatus

	// CurrentNodeID 在只有一个活动节点时记录该节点；多活动节点时为空。
	CurrentNodeID string

	// ActiveNodeIDs 是当前可推进的节点集合。
	ActiveNodeIDs []string

	// PendingJoins 是已经部分到达、但尚未满足全部入边的汇聚节点。
	PendingJoins []PendingJoin

	// CompletedNodeIDs 按完成顺序记录已推进完成的节点。
	CompletedNodeIDs []string

	// History 记录实例状态变化轨迹。
	History []HistoryEntry

	// Data 保存调用方附加的实例业务数据。
	Data map[string]any

	// StartedAt 表示实例启动时间。
	StartedAt time.Time

	// CompletedAt 表示实例完成时间。
	CompletedAt time.Time

	// CreatedAt 表示实例创建时间。
	CreatedAt time.Time

	// UpdatedAt 表示实例最近更新时间。
	UpdatedAt time.Time
}
