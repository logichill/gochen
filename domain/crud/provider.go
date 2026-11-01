package crud

// IDataSourceProvider 声明仓储或审计存储所在的数据源标识（D17）。
//
// 构造带审计的应用时，Application 要求仓储与审计存储都实现本接口，
// 且标识非空且相等，否则构造失败；本接口只用于一致性校验，
// 不替代 ITransactional 的事务能力。
type IDataSourceProvider interface {
	// DataSourceID 返回数据源标识；同一物理数据源必须返回相同值。
	DataSourceID() string
}

// INamedConditionProvider 声明仓储支持的命名条件（D17）。
//
// Application 构造时用它校验授权词汇中的 Named；只声明“名称存在”，
// 参数校验仍由仓储内部的翻译器负责。
type INamedConditionProvider interface {
	// HasNamedCondition 报告仓储是否注册了指定名称的命名条件。
	HasNamedCondition(name string) bool
}
