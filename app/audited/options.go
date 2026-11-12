package audited

import (
	"gochen/app"
	appcrud "gochen/app/crud"
	domaudited "gochen/domain/audited"
)

// Options 是 audited 应用的构造期配置。
//
// 在 crud 配置之上增加审计投影函数；软删、审计存储与生命周期端口由仓储能力自动装配。
type Options[T domaudited.IAuditedEntity[ID], ID comparable] struct {
	appcrud.Options[T, ID]
	Projector app.Projector[T]
}

// ExecOption 配置一次命令执行的审计元数据。
type ExecOption = appcrud.ExecOption

// WithReason 设置本次命令的审计原因。
func WithReason(reason string) ExecOption { return appcrud.WithReason(reason) }
