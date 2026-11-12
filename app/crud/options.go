package crud

import (
	"gochen/app"
	"gochen/app/internal/execution"
	"gochen/auth"
	"gochen/domain"
	"gochen/validate"
)

// Options 是 crud 应用的构造期配置；构造完成后全部冻结。
//
// Actions 只声明自定义动作；标准动作（read / list / create / update / delete，
// 配置 Audit 时另加 audit_read）由构造函数按能力自动加入。
//
// ActionPermissions 按动作名覆盖标准动作的默认权限码（<resource>:api:<action>），
// 允许多个动作共用一个权限码；键必须是本应用已声明的标准动作。
type Options[T domain.IEntity[ID], ID comparable] struct {
	Resource   string
	Authorizer auth.IAuthorizer
	// CommandGrant 将嵌套应用绑定到当前命令已经完成的完整授权判定。
	// 此类应用的 Transfer 返回 FailedPrecondition；两阶段迁移使用独立应用执行。
	CommandGrant      *app.CommandGrant
	Actions           []auth.Action
	ActionPermissions map[string]string
	Hooks             Hooks[T]
	Validator         validate.IValidator
	Limits            app.Limits
	Audit             app.IAuditRepository
}

// ExecOption 配置一次命令执行的审计元数据。
type ExecOption = execution.ExecOption

// WithReason 设置本次命令的审计原因。
func WithReason(reason string) ExecOption { return execution.WithReason(reason) }
