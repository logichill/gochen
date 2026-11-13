package crud

import (
	"gochen/app"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	"gochen/validate"
)

// DeleteMode 指定业务删除行为；零值不是有效写入配置。
type DeleteMode uint8

const (
	// DeleteSoft 只标记软删除；重复删除不会升级为物理清除。
	DeleteSoft DeleteMode = iota + 1
	// DeletePurge 通过完整 purge 授权、事务、钩子与审计流程物理清除。
	DeletePurge
)

// Options 是 crud 应用的构造期配置；构造完成后全部冻结。
//
// Actions 只声明自定义动作；标准动作由构造期配置确定。
// DeleteMode 必须显式选择；DeletePurge 使 Delete 执行 purge 动作、权限、钩子与审计。
// DeleteSoft 下 Delete 只标记软删，重复调用不会升级为永久清除。
// EnableRestore / EnableDeletedQuery 独立选择历史软删记录的管理能力，
// 不自动挂载 HTTP 路由；实体实现 ISoftDeletable 不会自动开启软删。
//
// ActionPermissions 按动作名覆盖标准动作的默认权限码（<resource>:api:<action>），
// 允许多个动作共用一个权限码；键必须是本应用已声明的标准动作。
type Options[T domain.IEntity[ID], ID comparable] struct {
	DeleteMode         DeleteMode
	EnableRestore      bool
	EnableDeletedQuery bool
	Resource           string
	Authorizer         auth.IAuthorizer
	Actions            []auth.Action
	ActionPermissions  map[string]string
	Hooks              Hooks[T]
	Validator          validate.IValidator
	Limits             app.Limits
	Audit              app.IAuditRepository
	Projector          app.Projector[T]
}

// ExecOption 配置一次命令执行的审计元数据。
type ExecOption = execution.ExecOption

// WithReason 设置本次命令的审计原因。
func WithReason(reason string) ExecOption { return execution.WithReason(reason) }
