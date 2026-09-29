package projection

import (
	"context"
	"reflect"

	"gochen/errors"
	"gochen/messaging"
)

// IProjectionRegistrar 定义 ID 无关的投影注册能力。
//
// 契约：
//   - 成功时返回非 nil 释放函数，只释放本次注册，重复调用成功，绝不删除同名的新注册；
//   - 注册失败但仍持有部分资源时也返回释放函数，调用方必须保留并重试清理；
//   - 释放等待并发清理或在途处理时响应 ctx 取消；进入清理后保持停用，允许重试；
//   - 释放失败时保留资源与名称占位，后续可用新的 context 重试同一函数。
type IProjectionRegistrar interface {
	RegisterProjectionAny(ctx context.Context, projection any) (messaging.UnsubscribeFunc, error)
}

// RegisterProjectionAny 按当前 manager 的 ID 类型注册投影，返回绑定本次注册的释放函数。
// 注册失败但清理未完成时也会返回非 nil 函数，调用方应重试释放。
func (pm *ProjectionManager[ID]) RegisterProjectionAny(ctx context.Context, projection any) (messaging.UnsubscribeFunc, error) {
	p, ok := projection.(IProjection[ID])
	if !ok || isNilProjection(p) {
		return nil, errors.NewCode(errors.InvalidInput, "projection type does not match projection manager ID type")
	}
	rt, err := pm.registerProjectionWithContext(ctx, p)
	if rt == nil {
		return nil, err
	}
	name := rt.status.Name
	return func(ctx context.Context) error {
		if ctx == nil {
			return errors.NewCode(errors.InvalidInput, "ctx is nil")
		}
		// 名称固定于注册时；不依赖调用方后续修改投影对象。
		err := pm.unregisterRuntime(ctx, name, rt)
		if errors.Code(err) == errors.NotFound {
			return nil
		}
		return err
	}, err
}

func isNilProjection(projection any) bool {
	if projection == nil {
		return true
	}

	v := reflect.ValueOf(projection)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
