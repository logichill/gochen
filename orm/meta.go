package orm

// AssociationKind 表示关联类型。
type AssociationKind string

const (
	AssociationBelongsTo  AssociationKind = "belongs_to"
	AssociationHasOne     AssociationKind = "has_one"
	AssociationHasMany    AssociationKind = "has_many"
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
	Model        any
	Table        string
	Fields       []FieldMeta
	Associations []AssociationMeta
	Tags         map[string]string
}

// Tag 返回模型级别的标签内容。
func (m *ModelMeta) Tag(key string) string {
	if m == nil || m.Tags == nil {
		return ""
	}
	return m.Tags[key]
}
