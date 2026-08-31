package gen

import (
	"strconv"
	"testing"

	"gochen/errors"
	"gochen/gen/snowflake"
)

func TestNewSnowflakeGeneratorFromConfigRequiresBothNodeIDs(t *testing.T) {
	tests := []struct {
		name string
		cfg  SnowflakeConfig
	}{
		{name: "missing datacenter", cfg: SnowflakeConfig{WorkerID: int64Pointer(1)}},
		{name: "missing worker", cfg: SnowflakeConfig{DatacenterID: int64Pointer(1)}},
		{name: "missing both", cfg: SnowflakeConfig{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator, err := NewSnowflakeGeneratorFromConfig(tt.cfg)
			if generator != nil {
				t.Fatalf("generator = %T; want nil", generator)
			}
			if !errors.Is(err, errors.InvalidInput) {
				t.Fatalf("error = %v; want InvalidInput", err)
			}
		})
	}
}

func TestNewSnowflakeGeneratorFromConfigValidatesNodeRanges(t *testing.T) {
	tests := []SnowflakeConfig{
		{DatacenterID: int64Pointer(-1), WorkerID: int64Pointer(0)},
		{DatacenterID: int64Pointer(32), WorkerID: int64Pointer(0)},
		{DatacenterID: int64Pointer(0), WorkerID: int64Pointer(-1)},
		{DatacenterID: int64Pointer(0), WorkerID: int64Pointer(32)},
	}

	for _, cfg := range tests {
		generator, err := NewSnowflakeGeneratorFromConfig(cfg)
		if generator != nil {
			t.Fatalf("generator = %T for %#v; want nil", generator, cfg)
		}
		if !errors.Is(err, errors.InvalidInput) {
			t.Fatalf("error = %v for %#v; want InvalidInput", err, cfg)
		}
	}
}

func TestNewSnowflakeGeneratorFromConfigAcceptsBoundaryNodes(t *testing.T) {
	for _, node := range []int64{0, 31} {
		generator, err := NewSnowflakeGeneratorFromConfig(SnowflakeConfig{
			DatacenterID: int64Pointer(node),
			WorkerID:     int64Pointer(node),
		})
		if err != nil {
			t.Fatalf("node %d: create generator: %v", node, err)
		}
		id, err := generator.Next()
		if err != nil {
			t.Fatalf("node %d: generate ID: %v", node, err)
		}
		parts := snowflake.Parse(id)
		if parts["datacenterID"] != node || parts["workerID"] != node {
			t.Fatalf("node %d: parsed ID = %#v", node, parts)
		}
	}
}

func TestSnowflakeStringGeneratorUsesDecimalSnowflakeIDs(t *testing.T) {
	generator, err := NewSnowflakeStringGenerator(1, 1)
	if err != nil {
		t.Fatalf("create generator: %v", err)
	}

	first, err := generator.Next()
	if err != nil {
		t.Fatalf("generate first ID: %v", err)
	}
	second, err := generator.Next()
	if err != nil {
		t.Fatalf("generate second ID: %v", err)
	}
	if first == second {
		t.Fatalf("expected unique IDs, got %q", first)
	}
	for _, id := range []string{first, second} {
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			t.Fatalf("expected decimal Snowflake ID, got %q: %v", id, err)
		}
		if parsed <= 0 {
			t.Fatalf("expected positive Snowflake ID, got %d", parsed)
		}
	}
}

func int64Pointer(value int64) *int64 { return &value }
