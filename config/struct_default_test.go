package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplyDefaultsByTag(t *testing.T) {
	type nested struct {
		Mode    string        `default:"release"`
		Timeout time.Duration `default:"30s"`
		Headers []string      `default:"A,B"`
	}
	type root struct {
		Name   string  `default:"demo"`
		Port   int     `default:"8080"`
		Debug  bool    `default:"true"`
		Nested *nested `default:"{}"`
	}

	cfg := &root{}
	ApplyDefaultsByTag(cfg)

	assert.Equal(t, "demo", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
	assert.True(t, cfg.Debug)
	if assert.NotNil(t, cfg.Nested) {
		assert.Equal(t, "release", cfg.Nested.Mode)
		assert.Equal(t, 30*time.Second, cfg.Nested.Timeout)
		assert.Equal(t, []string{"A", "B"}, cfg.Nested.Headers)
	}
}

func TestApplyDefaultsByTag_DoesNotOverrideExistingValue(t *testing.T) {
	type nested struct {
		Mode string `default:"release"`
	}
	type root struct {
		Name   string  `default:"demo"`
		Nested *nested `default:"{}"`
	}

	cfg := &root{
		Name:   "custom",
		Nested: &nested{Mode: "debug"},
	}
	ApplyDefaultsByTag(cfg)

	assert.Equal(t, "custom", cfg.Name)
	assert.Equal(t, "debug", cfg.Nested.Mode)
}

func TestApplyDefaultsByTag_EmptyStringSlice(t *testing.T) {
	type root struct {
		Headers []string `default:"[]"`
	}

	cfg := &root{}
	ApplyDefaultsByTag(cfg)

	assert.NotNil(t, cfg.Headers)
	assert.Empty(t, cfg.Headers)
}
