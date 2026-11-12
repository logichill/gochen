package deletion

import (
	appcrud "gochen/app/crud"
)

// Options 是删除生命周期的构造期配置。
//
// 审计是可选项；生命周期端口由构造参数显式提供。
type Options[T IEntity[ID], ID comparable] struct {
	appcrud.Options[T, ID]
}

// ExecOption 配置一次命令执行的审计元数据。
type ExecOption = appcrud.ExecOption

// WithReason 设置本次命令的审计原因。
func WithReason(reason string) ExecOption { return appcrud.WithReason(reason) }
