package orm

// ModelFactory 用于为 ORM 适配器生成一个“全新的零值模型实例”。
//
// 背景：
// - 某些 ORM（例如 GORM）在 Update/Delete 等操作中可能会修改传入的 model 实例（用于 hooks/语句构造等）。
// - 因此，不应在应用生命周期内长期复用同一个 model prototype 实例，否则会产生状态泄漏与隐式条件异常风险。
//
// 约定：
// - 推荐返回 `*Struct`（指向结构体的指针），以便适配器进行反射解析并正确识别 TableName 方法。
type ModelFactory func() any

// AssociationKind 表示关联类型。
type AssociationKind string

const (
	// AssociationBelongsTo 是常量。
	AssociationBelongsTo AssociationKind = "belongs_to"
	// AssociationHasOne 是常量。
	AssociationHasOne AssociationKind = "has_one"
	// AssociationHasMany 是常量。
	AssociationHasMany AssociationKind = "has_many"
	// AssociationManyToMany 是常量。
	AssociationManyToMany AssociationKind = "many_to_many"
)

// AssociationMeta 描述模型关联元信息。
type AssociationMeta struct {
	Name             string
	Kind             AssociationKind
	Target           any
	JoinTable        string
	ForeignKey       string
	ReferenceKey     string
	JoinForeignKey   string // 多对多/中间表时的本侧键
	JoinReferenceKey string // 多对多/中间表时的目标键
	Tags             map[string]string
}

// FieldMeta 描述字段元信息。
type FieldMeta struct {
	Name          string
	Column        string
	PrimaryKey    bool
	AutoIncrement bool
	Nullable      bool
	Unique        bool
	Indexes       []string
	DefaultValue  string
	Tags          map[string]string
}

// ModelMeta 描述模型级别元信息。
// Tags 可用于存放原始 orm/gorm 等标签内容，由适配器解析。
type ModelMeta struct {
	// ModelFactory 用于生成“全新的零值模型实例”，供适配器推导表名/Schema 等。
	ModelFactory ModelFactory

	Table        string
	Fields       []FieldMeta
	Associations []AssociationMeta
	Tags         map[string]string
}

func (m *ModelMeta) Tag(key string) string {
	if m == nil || m.Tags == nil {
		return ""
	}
	return m.Tags[key]
}
