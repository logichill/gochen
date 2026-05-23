package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplyEnvOverridesByYAMLPath(t *testing.T) {
	type childConfig struct {
		TableName string        `yaml:"table_name" env:"EVENT_STORE_TABLE"`
		TTL       time.Duration `yaml:"ttl"`
		Enabled   bool          `yaml:"enabled"`
		Headers   []string      `yaml:"headers"`
	}
	type rootConfig struct {
		Logging struct {
			Level string `yaml:"level"`
		} `yaml:"logging" envprefix:"LOG"`
		App struct {
			Environment string `yaml:"environment" env:"APP_ENV"`
		} `yaml:"app"`
		Event struct {
			Store childConfig `yaml:"store"`
		} `yaml:"event"`
	}

	cfg := &rootConfig{}
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("APP_ENV", "production")
	t.Setenv("EVENT_STORE_TABLE", "events")
	t.Setenv("EVENT_STORE_TTL", "5m")
	t.Setenv("EVENT_STORE_ENABLED", "true")
	t.Setenv("EVENT_STORE_HEADERS", "A,B")

	ApplyEnvOverridesByYAMLPath(cfg)

	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "production", cfg.App.Environment)
	assert.Equal(t, "events", cfg.Event.Store.TableName)
	assert.Equal(t, 5*time.Minute, cfg.Event.Store.TTL)
	assert.True(t, cfg.Event.Store.Enabled)
	assert.Equal(t, []string{"A", "B"}, cfg.Event.Store.Headers)
}
