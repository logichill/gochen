package gen

import (
	"strconv"

	"gochen/errors"
	"gochen/gen/snowflake"
)

// SnowflakeConfig 是部署级 Snowflake 节点配置。
// 同一时刻运行的每个应用副本必须使用唯一的 (datacenter_id, worker_id) 组合。
type SnowflakeConfig struct {
	DatacenterID *int64 `json:"datacenter_id" yaml:"datacenter_id"`
	WorkerID     *int64 `json:"worker_id" yaml:"worker_id"`
}

// NewSnowflakeGeneratorFromConfig 校验显式节点配置并创建长期存活的 generator。
func NewSnowflakeGeneratorFromConfig(cfg SnowflakeConfig) (IGenerator[int64], error) {
	if cfg.DatacenterID == nil || cfg.WorkerID == nil {
		return nil, errors.NewCode(errors.InvalidInput, "snowflake datacenter_id and worker_id are required").
			WithContext("replica_uniqueness", "each live replica must use a unique datacenter_id/worker_id pair")
	}
	return NewSnowflakeGenerator(*cfg.DatacenterID, *cfg.WorkerID)
}

// NewSnowflakeGenerator 创建带显式节点配置的 int64 Snowflake 生成器。
func NewSnowflakeGenerator(datacenterID, workerID int64, opts ...snowflake.Option) (IGenerator[int64], error) {
	generator, err := snowflake.NewGenerator(datacenterID, workerID, opts...)
	if err != nil {
		return nil, err
	}
	return generator, nil
}

type snowflakeStringGenerator struct {
	generator *snowflake.Generator
}

func (g *snowflakeStringGenerator) Next() (string, error) {
	id, err := g.generator.Next()
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

// NewSnowflakeStringGenerator 创建带显式节点配置的十进制字符串 Snowflake 生成器。
func NewSnowflakeStringGenerator(datacenterID, workerID int64, opts ...snowflake.Option) (IGenerator[string], error) {
	generator, err := snowflake.NewGenerator(datacenterID, workerID, opts...)
	if err != nil {
		return nil, err
	}
	return &snowflakeStringGenerator{generator: generator}, nil
}
