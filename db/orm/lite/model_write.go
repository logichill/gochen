package lite

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"gochen/db/orm"
	dbsql "gochen/db/sql/sqlbuilder"
	"gochen/errors"
)

// Create 创建对象并写入存储。
//
// 说明：
// - Create 插入记录（支持批量）。
//
// 参数：
// - entities：要处理的实体列表。
func (m *model) Create(ctx context.Context, entities ...any) error {
	if len(entities) == 0 {
		return nil
	}

	// 以第一个实体的类型构建字段映射
	first := entities[0]
	sm := m.orm.structMetaForValue(first)
	if sm == nil {
		return errors.NewCode(errors.InvalidInput, "basic.Model.Create: unsupported entity type").
			WithContext("entity_type", fmt.Sprintf("%T", first))
	}

	cols, insertFields := sm.insertableColumns()
	if len(cols) == 0 {
		return errors.NewCode(errors.InvalidInput, "basic.Model.Create: no insertable columns").
			WithContext("entity_type", fmt.Sprintf("%T", first))
	}

	builder := m.orm.sql.InsertInto(m.table).Columns(cols...)

	// 记录第一个实体的基础类型，用于后续一致性校验
	firstType := sm.typ

	for _, e := range entities {
		val, ok := writableStructValue(e)
		if !ok {
			return errors.NewCode(errors.InvalidInput, "basic.Model.Create: entity must be struct or *struct").
				WithContext("entity_type", fmt.Sprintf("%T", e))
		}
		// 禁止批量插入混合类型：不同类型的字段 index 不同，用第一个类型的
		// insertFields 去读其他类型的字段会写入错误数据。
		if val.Type() != firstType {
			return errors.NewCode(errors.InvalidInput, "basic.Model.Create: all entities must be the same type").
				WithContext("expected_type", firstType.String()).
				WithContext("actual_type", fmt.Sprintf("%T", e))
		}

		rowVals := make([]any, len(insertFields))
		for i, fi := range insertFields {
			fv := fieldByIndexSafe(val, fi.Index)
			rowVals[i] = dbWriteValue(fv)
		}
		builder = builder.Values(rowVals...)
	}

	_, err := builder.Exec(ctx)
	return err
}

// Save 保存数据。
func (m *model) Save(ctx context.Context, entity any, opts ...orm.QueryOption) error {
	builder, err := m.buildSaveBuilder(entity, opts...)
	if err != nil {
		return err
	}
	_, err = builder.Exec(ctx)
	return err
}

// SaveWithResult 保存带结果。
func (m *model) SaveWithResult(ctx context.Context, entity any, opts ...orm.QueryOption) (sql.Result, error) {
	builder, err := m.buildSaveBuilder(entity, opts...)
	if err != nil {
		return nil, err
	}
	return builder.Exec(ctx)
}

// buildSaveBuilder 构建 Save/SaveWithResult 所需的 UPDATE builder。
func (m *model) buildSaveBuilder(entity any, opts ...orm.QueryOption) (dbsql.IUpdateBuilder, error) {
	val, ok := writableStructValue(entity)
	if !ok {
		return nil, errors.NewCode(errors.InvalidInput, "basic.Model.Save: entity must be struct or *struct").
			WithContext("entity_type", fmt.Sprintf("%T", entity))
	}

	sm := m.orm.structMetaForValue(entity)
	if sm == nil {
		return nil, errors.NewCode(errors.InvalidInput, "basic.Model.Save: unsupported entity type").
			WithContext("entity_type", fmt.Sprintf("%T", entity))
	}

	qo := orm.CollectQueryOptions(opts...)
	builder := m.orm.sql.Update(m.table)

	for _, fi := range sm.fields {
		// 跳过自增主键
		if fi.PrimaryKey && fi.AutoIncrement {
			continue
		}
		fv := fieldByIndexSafe(val, fi.Index)
		if !fv.IsValid() {
			continue
		}
		builder = builder.Set(fi.Column, dbWriteValue(fv))
	}

	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}

	return builder, nil
}

// UpdateValues 更新对象并写入存储。
//
// 说明：
// - UpdateValues 根据 values 与 QueryOptions 进行更新。
func (m *model) UpdateValues(ctx context.Context, values map[string]any, opts ...orm.QueryOption) error {
	if len(values) == 0 {
		return nil
	}

	qo := orm.CollectQueryOptions(opts...)
	builder := m.orm.sql.Update(m.table).SetMap(values)
	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}

	_, err := builder.Exec(ctx)
	return err
}

type noopResult int64

func (r noopResult) LastInsertId() (int64, error) { return 0, nil }

func (r noopResult) RowsAffected() (int64, error) { return int64(r), nil }

// UpdateValuesWithResult 更新对象并写入存储。
func (m *model) UpdateValuesWithResult(ctx context.Context, values map[string]any, opts ...orm.QueryOption) (sql.Result, error) {
	if len(values) == 0 {
		return noopResult(0), nil
	}

	qo := orm.CollectQueryOptions(opts...)
	builder := m.orm.sql.Update(m.table).SetMap(values)
	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}

	return builder.Exec(ctx)
}

// Delete 删除对象并同步到存储。
//
// 说明：
// - Delete 根据 QueryOptions 删除记录。
func (m *model) Delete(ctx context.Context, opts ...orm.QueryOption) error {
	qo := orm.CollectQueryOptions(opts...)
	if len(qo.Where) == 0 {
		return errors.NewCode(errors.InvalidInput, "basic.Orm: delete without where is not allowed")
	}

	builder := m.orm.sql.DeleteFrom(m.table)
	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}
	if qo.Limit > 0 {
		builder = builder.Limit(qo.Limit)
	}
	_, err := builder.Exec(ctx)
	return err
}

// DeleteWithResult 删除对象并同步到存储。
func (m *model) DeleteWithResult(ctx context.Context, opts ...orm.QueryOption) (sql.Result, error) {
	qo := orm.CollectQueryOptions(opts...)
	if len(qo.Where) == 0 {
		return nil, errors.NewCode(errors.InvalidInput, "basic.Orm: delete without where is not allowed")
	}

	builder := m.orm.sql.DeleteFrom(m.table)
	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}
	if qo.Limit > 0 {
		builder = builder.Limit(qo.Limit)
	}
	return builder.Exec(ctx)
}

func writableStructValue(entity any) (reflect.Value, bool) {
	val := reflect.ValueOf(entity)
	if !val.IsValid() {
		return reflect.Value{}, false
	}
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return reflect.Value{}, false
		}
		val = val.Elem()
	} else {
		copyVal := reflect.New(val.Type()).Elem()
		copyVal.Set(val)
		val = copyVal
	}
	if val.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return val, true
}

func dbWriteValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() != reflect.Ptr && !v.Type().Implements(driverValuerType) && v.CanAddr() {
		addr := v.Addr()
		if addr.Type().Implements(driverValuerType) {
			return addr.Interface()
		}
	}
	return v.Interface()
}
