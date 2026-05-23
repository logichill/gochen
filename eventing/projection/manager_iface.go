package projection

import (
	"context"

	"gochen/errors"
)

// IProjectionRegistrar 定义 ID 无关的投影注册能力。
//
// 说明：
// - Host 模块运行时只需要把显式声明的 projection 实例交给组合根注入的 manager；
// - 具体 manager 再根据自己的 ID 类型断言 projection.IProjection[ID]，避免 Host 固化 int64。
type IProjectionRegistrar interface {
	RegisterProjectionAny(ctx context.Context, projection any) error
}

// RegisterProjectionAny 按当前 manager 的 ID 类型注册 projection。
func (pm *ProjectionManager[ID]) RegisterProjectionAny(ctx context.Context, projection any) error {
	p, ok := projection.(IProjection[ID])
	if !ok || p == nil {
		return errors.NewCode(errors.InvalidInput, "projection type does not match projection manager ID type")
	}
	return pm.RegisterProjectionWithContext(ctx, p)
}
