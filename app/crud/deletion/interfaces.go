package deletion

import (
	"context"

	appcrud "gochen/app/crud"
	"gochen/domain"
)

// IDeletionApplication 是独立的删除生命周期能力端口：
// 恢复、永久清理与已删除查询。
//
// 传输层（如 rest）据此判断是否挂载 /:id/restore、/:id/purge 与 /deleted，
// 接口归 app 层所有，避免框架内出现多份需要手工同步的副本。
type IDeletionApplication[T domain.IEntity[ID], ID comparable] interface {
	Delete(ctx context.Context, id ID) error
	DeleteBulk(ctx context.Context, ids []ID) error
	Restore(ctx context.Context, id ID, opts ...appcrud.ExecOption) (T, error)
	Purge(ctx context.Context, id ID, opts ...appcrud.ExecOption) error
	ListDeleted(ctx context.Context, offset, limit int) ([]T, error)
}
