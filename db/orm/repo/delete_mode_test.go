package repo

import (
	"context"
	"testing"

	"gochen/db/orm"
)

type deleteModeModel struct {
	deleted      bool
	updateCalled bool
}

// Meta result：返回的实例（类型：*orm.ModelMeta）。
//
// 返回：
func (m *deleteModeModel) Meta() *orm.ModelMeta { return &orm.ModelMeta{Table: "t"} }

// Capabilities result：测试返回值（类型：orm.Capabilities）。
//
// 返回：
func (m *deleteModeModel) Capabilities() orm.Capabilities { return orm.Capabilities{} }

// First context.Context：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - v：参数值（具体语义见函数上下文）（类型：any）
// - args：查询选项（用于补充 WHERE/JOIN/分页/排序等）（类型：...orm.QueryOption）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *deleteModeModel) First(context.Context, any, ...orm.QueryOption) error {
	panic("not used")
}

// Find 从存储中查询对象。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - v：参数值（具体语义见函数上下文）（类型：any）
// - args：查询选项（用于补充 WHERE/JOIN/分页/排序等）（类型：...orm.QueryOption）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *deleteModeModel) Find(context.Context, any, ...orm.QueryOption) error { panic("not used") }

// Count 返回匹配条件的记录数。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - args：查询选项（用于补充 WHERE/JOIN/分页/排序等）（类型：...orm.QueryOption）
//
// 返回：
// - result1：数值结果
// - err：错误信息（nil 表示成功）
func (m *deleteModeModel) Count(context.Context, ...orm.QueryOption) (int64, error) {
	panic("not used")
}

// Create 创建对象并写入存储。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - args：可变参数（类型：...any）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *deleteModeModel) Create(context.Context, ...any) error { panic("not used") }

// Save context.Context：上下文（用于取消、超时与链路信息）。
//
// 参数：
// - v：参数值（具体语义见函数上下文）（类型：any）
// - args：查询选项（用于补充 WHERE/JOIN/分页/排序等）（类型：...orm.QueryOption）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *deleteModeModel) Save(context.Context, any, ...orm.QueryOption) error { panic("not used") }

// UpdateValues 更新对象并写入存储。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - attrs：属性/参数集合（类型：map[string]any）
// - args：查询选项（用于补充 WHERE/JOIN/分页/排序等）（类型：...orm.QueryOption）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *deleteModeModel) UpdateValues(context.Context, map[string]any, ...orm.QueryOption) error {
	m.updateCalled = true
	return nil
}

// Delete 删除对象并同步到存储。
//
// 参数：
// - context.Context：上下文（用于取消、超时与链路信息）
// - args：查询选项（用于补充 WHERE/JOIN/分页/排序等）（类型：...orm.QueryOption）
//
// 返回：
// - err：错误信息（nil 表示成功）
func (m *deleteModeModel) Delete(context.Context, ...orm.QueryOption) error {
	m.deleted = true
	return nil
}

// Association 返回关联查询对象（测试 stub）。
//
// 参数：
// - owner：关联主对象
// - name：关联名
//
// 返回：
// - result：关联查询对象
func (m *deleteModeModel) Association(owner any, name string) orm.IAssociation {
	_ = owner
	_ = name
	return nil
}

// TestRepo_Purge_AlwaysHardDeletes 验证 Repo Purge AlwaysHardDeletes。
func TestRepo_Purge_AlwaysHardDeletes(t *testing.T) {
	m := &deleteModeModel{}
	r := &Repo[*auditedEntity, int64]{
		model:          m,
		softDelete:     true,
		auditFields:    false,
		defaultActor:   "system",
		softDeleteCols: softDeleteColumns{DeletedAt: "deleted_at", DeletedBy: "deleted_by"},
	}

	if err := r.Purge(context.Background(), int64(1)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.deleted {
		t.Fatalf("expected permanent delete to call model.Delete")
	}
	if m.updateCalled {
		t.Fatalf("expected permanent delete to not call model.UpdateValues")
	}
}
