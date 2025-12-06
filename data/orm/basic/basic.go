package basic

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"

	dbcore "gochen/data/db"
	dbsql "gochen/data/db/sql"
	"gochen/data/orm"
)

// Orm 是基于 gochen/data/db + gochen/data/db/sql 的轻量 IOrm 实现。
//
// 设计目标：
//   - 不依赖具体 ORM（如 gorm），直接在 DB 抽象之上工作；
//   - 覆盖 IAM / LLM 模块常见的查询与增删改需求；
//   - 保持能力最小化，复杂特性（高级 Preload、自定义表达式等）逐步按需扩展。
type Orm struct {
	db   dbcore.IDatabase
	sql  dbsql.ISql
	caps orm.Capabilities

	mu        sync.RWMutex
	structMap map[reflect.Type]*structMeta
}

// New 创建一个基于指定 IDatabase 的 Orm 适配器。
func New(db dbcore.IDatabase) orm.IOrm {
	return &Orm{
		db:  db,
		sql: dbsql.New(db),
		caps: orm.NewCapabilities(
			orm.CapabilityBasicCRUD,
			orm.CapabilityQuery,
			orm.CapabilityTransaction,
		),
		structMap: make(map[reflect.Type]*structMeta),
	}
}

// Capabilities 返回适配器支持的能力。
func (o *Orm) Capabilities() orm.Capabilities { return o.caps }

// WithContext 当前实现不在 Orm 上持有 context，直接返回自身即可。
func (o *Orm) WithContext(ctx context.Context) orm.IOrm { // nolint: revive
	_ = ctx
	return o
}

// Model 返回模型级操作入口。
func (o *Orm) Model(meta *orm.ModelMeta) orm.IModel {
	if meta == nil {
		panic("basic.Orm: ModelMeta cannot be nil")
	}

	table := meta.Table
	if table == "" {
		// 如果模型实现了 TableName()，优先使用
		if meta.Model != nil {
			if tn, ok := tryGetTableName(meta.Model); ok {
				table = tn
			}
		}
	}
	if table == "" {
		panic("basic.Orm: table name is empty")
	}

	return &model{
		orm:   o,
		meta:  meta,
		table: table,
	}
}

// Begin 开启事务会话。
func (o *Orm) Begin(ctx context.Context) (orm.IOrmSession, error) {
	tx, err := o.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &session{
		Orm: New(tx).(*Orm), // 内部会重新构造 sqlImpl
		tx:  tx,
	}, nil
}

// BeginTx 开启带选项的事务会话。
func (o *Orm) BeginTx(ctx context.Context, opts *sql.TxOptions) (orm.IOrmSession, error) {
	tx, err := o.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &session{
		Orm: New(tx).(*Orm),
		tx:  tx,
	}, nil
}

// Database 返回底层数据库抽象。
func (o *Orm) Database() dbcore.IDatabase { return o.db }

// Raw 返回底层实现（此处为 dbcore.IDatabase）。
func (o *Orm) Raw() any { return o.db }

// session 实现 IOrmSession，委托给内部 Orm，并持有事务以便 Commit/Rollback。
type session struct {
	*Orm
	tx dbcore.ITransaction
}

// Commit 提交事务。
func (s *session) Commit() error {
	if s.tx == nil {
		return fmt.Errorf("basic.session: tx is nil")
	}
	return s.tx.Commit()
}

// Rollback 回滚事务。
func (s *session) Rollback() error {
	if s.tx == nil {
		return fmt.Errorf("basic.session: tx is nil")
	}
	return s.tx.Rollback()
}

// ------------------------------------------------------------------------
// model 实现 orm.IModel
// ------------------------------------------------------------------------

type model struct {
	orm   *Orm
	meta  *orm.ModelMeta
	table string
}

func (m *model) Meta() *orm.ModelMeta           { return m.meta }
func (m *model) Capabilities() orm.Capabilities { return m.orm.caps }

// First 查询单条记录。
func (m *model) First(ctx context.Context, dest any, opts ...orm.QueryOption) error {
	qo := orm.CollectQueryOptions(opts...)
	columns := qo.Select
	if len(columns) == 0 {
		columns = []string{"*"}
	}

	builder := m.orm.sql.Select(columns...)
	tableExpr := buildTableExpr(m.table, qo.Joins)
	builder = builder.From(tableExpr)

	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}
	if len(qo.GroupBy) > 0 {
		builder = builder.GroupBy(qo.GroupBy...)
	}
	if len(qo.OrderBy) > 0 {
		builder = builder.OrderBy(buildOrderByExpr(qo.OrderBy))
	}
	// First 至少限制一条
	if qo.Limit > 0 {
		builder = builder.Limit(qo.Limit)
	} else {
		builder = builder.Limit(1)
	}
	if qo.Offset > 0 {
		builder = builder.Offset(qo.Offset)
	}

	rows, err := builder.Query(ctx)
	if err != nil {
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return orm.ErrNotFound
	}

	if err := scanRowsIntoDest(rows, dest, m.orm); err != nil {
		return err
	}

	return nil
}

// Find 查询多条记录。
func (m *model) Find(ctx context.Context, dest any, opts ...orm.QueryOption) error {
	qo := orm.CollectQueryOptions(opts...)
	columns := qo.Select
	if len(columns) == 0 {
		columns = []string{"*"}
	}

	builder := m.orm.sql.Select(columns...)
	tableExpr := buildTableExpr(m.table, qo.Joins)
	builder = builder.From(tableExpr)

	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}
	if len(qo.GroupBy) > 0 {
		builder = builder.GroupBy(qo.GroupBy...)
	}
	if len(qo.OrderBy) > 0 {
		builder = builder.OrderBy(buildOrderByExpr(qo.OrderBy))
	}
	if qo.Limit > 0 {
		builder = builder.Limit(qo.Limit)
	}
	if qo.Offset > 0 {
		builder = builder.Offset(qo.Offset)
	}

	rows, err := builder.Query(ctx)
	if err != nil {
		return err
	}
	defer rows.Close()

	return scanRowsIntoDest(rows, dest, m.orm)
}

// Count 统计数量（忽略 Select/GroupBy，只做简单 COUNT(*)）。
func (m *model) Count(ctx context.Context, opts ...orm.QueryOption) (int64, error) {
	qo := orm.CollectQueryOptions(opts...)

	builder := m.orm.sql.Select("COUNT(*)")
	tableExpr := buildTableExpr(m.table, qo.Joins)
	builder = builder.From(tableExpr)
	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}

	row := builder.QueryRow(ctx)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// Create 插入记录（支持批量）。
func (m *model) Create(ctx context.Context, entities ...any) error {
	if len(entities) == 0 {
		return nil
	}

	// 以第一个实体的类型构建字段映射
	first := entities[0]
	sm := m.orm.structMetaForValue(first)
	if sm == nil {
		return fmt.Errorf("basic.Model.Create: unsupported entity type %T", first)
	}

	cols, insertFields := sm.insertableColumns()
	if len(cols) == 0 {
		return fmt.Errorf("basic.Model.Create: no insertable columns for %T", first)
	}

	builder := m.orm.sql.InsertInto(m.table).Columns(cols...)

	for _, e := range entities {
		val := reflect.ValueOf(e)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		if !val.IsValid() || val.Kind() != reflect.Struct {
			return fmt.Errorf("basic.Model.Create: entity must be struct or *struct, got %T", e)
		}

		rowVals := make([]any, len(insertFields))
		for i, fi := range insertFields {
			fv := fieldByIndexSafe(val, fi.Index)
			if !fv.IsValid() {
				rowVals[i] = nil
				continue
			}
			rowVals[i] = fv.Interface()
		}
		builder = builder.Values(rowVals...)
	}

	_, err := builder.Exec(ctx)
	return err
}

// Save 根据 QueryOptions 执行更新（通常结合主键或条件）。
func (m *model) Save(ctx context.Context, entity any, opts ...orm.QueryOption) error {
	val := reflect.ValueOf(entity)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if !val.IsValid() || val.Kind() != reflect.Struct {
		return fmt.Errorf("basic.Model.Save: entity must be struct or *struct, got %T", entity)
	}

	sm := m.orm.structMetaForValue(entity)
	if sm == nil {
		return fmt.Errorf("basic.Model.Save: unsupported entity type %T", entity)
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
		builder = builder.Set(fi.Column, fv.Interface())
	}

	for _, w := range qo.Where {
		builder = builder.Where(w.Expr, w.Args...)
	}

	_, err := builder.Exec(ctx)
	return err
}

// UpdateValues 根据 values 与 QueryOptions 进行更新。
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

// Delete 根据 QueryOptions 删除记录。
func (m *model) Delete(ctx context.Context, opts ...orm.QueryOption) error {
	qo := orm.CollectQueryOptions(opts...)
	if len(qo.Where) == 0 {
		return fmt.Errorf("basic.Orm: delete without where is not allowed")
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

// Association 当前仅提供占位实现，后续可按需扩展为多对多关联写入。
func (m *model) Association(owner any, name string) orm.IAssociation {
	return &unsupportedAssociation{name: name}
}

// ------------------------------------------------------------------------
// Association 占位实现：暂不支持关联写入，直接返回 ErrUnsupported。
// ------------------------------------------------------------------------

type unsupportedAssociation struct {
	name string
}

func (a *unsupportedAssociation) Name() string { return a.name }
func (a *unsupportedAssociation) Owner() any   { return nil }

func (a *unsupportedAssociation) Append(ctx context.Context, targets ...any) error {
	_ = ctx
	_ = targets
	return orm.ErrUnsupported
}

func (a *unsupportedAssociation) Replace(ctx context.Context, targets ...any) error {
	_ = ctx
	_ = targets
	return orm.ErrUnsupported
}

func (a *unsupportedAssociation) Delete(ctx context.Context, targets ...any) error {
	_ = ctx
	_ = targets
	return orm.ErrUnsupported
}

func (a *unsupportedAssociation) Clear(ctx context.Context) error {
	_ = ctx
	return orm.ErrUnsupported
}

// ------------------------------------------------------------------------
// 结构体元信息与扫描工具
// ------------------------------------------------------------------------

type fieldInfo struct {
	Column        string
	Index         []int
	PrimaryKey    bool
	AutoIncrement bool
}

type structMeta struct {
	typ          reflect.Type
	fields       []fieldInfo
	columnToInfo map[string]fieldInfo
}

// insertableColumns 返回可用于 INSERT 的列及对应字段。
func (sm *structMeta) insertableColumns() ([]string, []fieldInfo) {
	var cols []string
	var fields []fieldInfo
	for _, f := range sm.fields {
		// 自增主键默认交给数据库生成
		if f.PrimaryKey && f.AutoIncrement {
			continue
		}
		cols = append(cols, f.Column)
		fields = append(fields, f)
	}
	return cols, fields
}

// structMetaForValue 构建或获取指定值类型的 structMeta。
func (o *Orm) structMetaForValue(v any) *structMeta {
	t := reflect.TypeOf(v)
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	o.mu.RLock()
	if sm, ok := o.structMap[t]; ok {
		o.mu.RUnlock()
		return sm
	}
	o.mu.RUnlock()

	sm := buildStructMeta(t)
	o.mu.Lock()
	o.structMap[t] = sm
	o.mu.Unlock()
	return sm
}

func buildStructMeta(t reflect.Type) *structMeta {
	sm := &structMeta{
		typ:          t,
		columnToInfo: make(map[string]fieldInfo),
	}

	var walk func(reflect.Type, []int)
	walk = func(cur reflect.Type, prefix []int) {
		for i := 0; i < cur.NumField(); i++ {
			f := cur.Field(i)
			// 跳过未导出字段
			if f.PkgPath != "" {
				continue
			}

			index := append(append([]int(nil), prefix...), i)

			if f.Anonymous && f.Type.Kind() == reflect.Struct && !isTimeType(f.Type) {
				// 内嵌结构体（例如 entity.Entity），递归展开
				walk(f.Type, index)
				continue
			}

			// 只收集“标量”字段，跳过切片/映射/结构体（time.Time 除外）
			if !isScalarDBField(f.Type) {
				continue
			}

			col, pk, auto := parseColumnTag(f)
			if col == "" {
				col = toSnakeCase(f.Name)
			}

			info := fieldInfo{
				Column:        col,
				Index:         index,
				PrimaryKey:    pk,
				AutoIncrement: auto,
			}
			sm.fields = append(sm.fields, info)
			// 后来的同名列覆盖之前的定义（以最内层为准）
			sm.columnToInfo[col] = info
		}
	}

	walk(t, nil)
	return sm
}

func isScalarDBField(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if isTimeType(t) {
		return true
	}
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	default:
		return false
	}
}

func isTimeType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.PkgPath() == "time" && t.Name() == "Time"
}

func parseColumnTag(f reflect.StructField) (column string, primaryKey, autoIncrement bool) {
	gormTag := f.Tag.Get("gorm")
	if gormTag != "" {
		parts := strings.Split(gormTag, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, "column:") {
				column = strings.TrimPrefix(part, "column:")
			}
			if strings.EqualFold(part, "primaryKey") || strings.EqualFold(part, "primary_key") {
				primaryKey = true
			}
			if strings.EqualFold(part, "autoIncrement") || strings.EqualFold(part, "autoincrement") {
				autoIncrement = true
			}
		}
	}

	if column == "" {
		if dbTag := f.Tag.Get("db"); dbTag != "" {
			column = dbTag
		} else if jsonTag := f.Tag.Get("json"); jsonTag != "" {
			column = strings.Split(jsonTag, ",")[0]
		}
	}

	return column, primaryKey, autoIncrement
}

func toSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, '_')
		}
		result = append(result, rune(strings.ToLower(string(r))[0]))
	}
	return string(result)
}

// scanRowsIntoDest 将 rows 扫描到 dest 中。
// 支持 dest 为 *T 或 *[]T。
func scanRowsIntoDest(rows dbcore.IRows, dest any, orm *Orm) error {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("basic.scanRowsIntoDest: dest must be non-nil pointer")
	}

	elem := rv.Elem()
	switch elem.Kind() {
	case reflect.Slice:
		elemType := elem.Type().Elem()
		for rows.Next() {
			item := reflect.New(elemType).Elem()
			if err := scanOneRow(rows, item, orm); err != nil {
				return err
			}
			elem.Set(reflect.Append(elem, item))
		}
		return rows.Err()
	case reflect.Struct:
		// 已在 First 手动 Next() 过一行，这里直接扫描当前行
		return scanOneRow(rows, elem, orm)
	default:
		return fmt.Errorf("basic.scanRowsIntoDest: unsupported dest element kind %s", elem.Kind())
	}
}

func scanOneRow(rows dbcore.IRows, v reflect.Value, orm *Orm) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	destPtrs := make([]any, len(cols))

	// 按目标类型构建 structMeta
	sm := orm.structMetaForValue(v.Addr().Interface())

	// 如果不是 struct（理论上不会发生），降级为丢弃
	if sm == nil {
		for i := range destPtrs {
			var tmp any
			destPtrs[i] = &tmp
		}
		return rows.Scan(destPtrs...)
	}

	for i, col := range cols {
		if fi, ok := sm.columnToInfo[col]; ok {
			fv := fieldByIndexSafe(v, fi.Index)
			if !fv.IsValid() || !fv.CanSet() {
				var tmp any
				destPtrs[i] = &tmp
				continue
			}
			// 为 Scan 准备一个指针
			destPtrs[i] = fv.Addr().Interface()
		} else {
			var tmp any
			destPtrs[i] = &tmp
		}
	}

	if err := rows.Scan(destPtrs...); err != nil {
		return err
	}
	return nil
}

func fieldByIndexSafe(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct || i < 0 || i >= v.NumField() {
			return reflect.Value{}
		}
		v = v.Field(i)
	}
	return v
}

func buildTableExpr(base string, joins []orm.Join) string {
	if len(joins) == 0 {
		return base
	}
	var sb strings.Builder
	sb.WriteString(base)
	for _, j := range joins {
		sb.WriteRune(' ')
		sb.WriteString(j.Expr)
	}
	return sb.String()
}

func buildOrderByExpr(orders []orm.OrderBy) string {
	if len(orders) == 0 {
		return ""
	}
	parts := make([]string, 0, len(orders))
	for _, o := range orders {
		if o.Column == "" {
			continue
		}
		if o.Desc {
			parts = append(parts, o.Column+" DESC")
		} else {
			parts = append(parts, o.Column+" ASC")
		}
	}
	return strings.Join(parts, ", ")
}

// tryGetTableName 尝试从模型实例上调用 TableName()。
func tryGetTableName(model any) (string, bool) {
	if model == nil {
		return "", false
	}
	v := reflect.ValueOf(model)
	if !v.IsValid() {
		return "", false
	}

	if v.Kind() == reflect.Ptr && v.IsNil() {
		v = reflect.New(v.Type().Elem())
	}

	m, ok := v.Interface().(interface{ TableName() string })
	if ok {
		return m.TableName(), true
	}

	t := v.Type()
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return "", false
	}
	// 如果类型本身实现了 TableName（值接收者）
	if m2, ok := reflect.New(t).Interface().(interface{ TableName() string }); ok {
		return m2.TableName(), true
	}

	return "", false
}
