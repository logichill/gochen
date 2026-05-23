package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConfigProvider 模拟配置提供者
type mockConfigProvider struct {
	values map[string]any
}

func newMockConfigProvider(values map[string]any) *mockConfigProvider {
	return &mockConfigProvider{values: values}
}

func (p *mockConfigProvider) Get(key string) any {
	return p.values[key]
}

func (p *mockConfigProvider) GetString(key string, defaultValue string) string {
	if v, ok := p.values[key].(string); ok {
		return v
	}
	return defaultValue
}

func (p *mockConfigProvider) GetStringStrict(key string) (string, error) {
	if v, ok := p.values[key].(string); ok {
		return v, nil
	}
	return "", nil
}

func (p *mockConfigProvider) GetInt(key string, defaultValue int) int {
	if v, ok := p.values[key].(int); ok {
		return v
	}
	return defaultValue
}

func (p *mockConfigProvider) GetIntStrict(key string) (int, error) {
	if v, ok := p.values[key].(int); ok {
		return v, nil
	}
	return 0, nil
}

func (p *mockConfigProvider) GetInt64(key string, defaultValue int64) int64 {
	if v, ok := p.values[key].(int64); ok {
		return v
	}
	return defaultValue
}

func (p *mockConfigProvider) GetFloat64(key string, defaultValue float64) float64 {
	if v, ok := p.values[key].(float64); ok {
		return v
	}
	return defaultValue
}

func (p *mockConfigProvider) GetBool(key string, defaultValue bool) bool {
	if v, ok := p.values[key].(bool); ok {
		return v
	}
	return defaultValue
}

func (p *mockConfigProvider) GetBoolStrict(key string) (bool, error) {
	if v, ok := p.values[key].(bool); ok {
		return v, nil
	}
	return false, nil
}

func (p *mockConfigProvider) GetDuration(key string, defaultValue time.Duration) time.Duration {
	if v, ok := p.values[key].(time.Duration); ok {
		return v
	}
	return defaultValue
}

func (p *mockConfigProvider) GetDurationStrict(key string) (time.Duration, error) {
	if v, ok := p.values[key].(time.Duration); ok {
		return v, nil
	}
	return 0, nil
}

func (p *mockConfigProvider) GetStringSlice(key string, defaultValue []string) []string {
	return defaultValue
}

func (p *mockConfigProvider) GetStringMap(key string, defaultValue map[string]string) map[string]string {
	return defaultValue
}

func (p *mockConfigProvider) Bind(key string, target any) error {
	return nil
}

func (p *mockConfigProvider) Has(key string) bool {
	_, ok := p.values[key]
	return ok
}

func (p *mockConfigProvider) AllSettings() map[string]any {
	return p.values
}

func TestConfigSchema_ValidateWithSchema_Required(t *testing.T) {
	provider := newMockConfigProvider(map[string]any{
		"server.port": 8080,
	})

	schema := ConfigSchema{
		Required: []string{"server.port", "database.dsn"},
	}

	errors := ValidateWithSchema(provider, schema)

	require.Len(t, errors, 1)
	assert.Equal(t, "database.dsn", errors[0].Key)
	assert.Contains(t, errors[0].Message, "required")
}

func TestConfigSchema_ValidateWithSchema_RequiredWithDefault(t *testing.T) {
	provider := newMockConfigProvider(map[string]any{
		"server.port": 8080,
	})

	schema := ConfigSchema{
		Required: []string{"server.port", "database.dsn"},
		Defaults: map[string]any{
			"database.dsn": "sqlite://memory",
		},
	}

	errors := ValidateWithSchema(provider, schema)

	assert.Empty(t, errors)
}

func TestConfigSchema_ValidateWithSchema_Validators(t *testing.T) {
	provider := newMockConfigProvider(map[string]any{
		"server.port": 70000, // out of range
	})

	schema := ConfigSchema{
		Validators: map[string]ValidatorFunc{
			"server.port": RangeValidator(1, 65535),
		},
	}

	errors := ValidateWithSchema(provider, schema)

	require.Len(t, errors, 1)
	assert.Equal(t, "server.port", errors[0].Key)
	assert.Contains(t, errors[0].Message, "out of range")
}

func TestRangeValidator(t *testing.T) {
	validator := RangeValidator(1, 100)

	assert.NoError(t, validator(50))
	assert.NoError(t, validator(1))
	assert.NoError(t, validator(100))
	assert.Error(t, validator(0))
	assert.Error(t, validator(101))
}

func TestMinValidator(t *testing.T) {
	validator := MinValidator(10)

	assert.NoError(t, validator(10))
	assert.NoError(t, validator(100))
	assert.Error(t, validator(9))
}

func TestMaxValidator(t *testing.T) {
	validator := MaxValidator(100)

	assert.NoError(t, validator(100))
	assert.NoError(t, validator(0))
	assert.Error(t, validator(101))
}

func TestNotEmptyValidator(t *testing.T) {
	validator := NotEmptyValidator()

	assert.NoError(t, validator("hello"))
	assert.Error(t, validator(""))
}

func TestOneOfValidator(t *testing.T) {
	validator := OneOfValidator("debug", "info", "warn", "error")

	assert.NoError(t, validator("debug"))
	assert.NoError(t, validator("info"))
	assert.Error(t, validator("trace"))
	assert.Error(t, validator(""))
}

func TestPatternValidator(t *testing.T) {
	validator := PatternValidator(`^\d{3}-\d{4}$`)

	assert.NoError(t, validator("123-4567"))
	assert.Error(t, validator("12-345"))
	assert.Error(t, validator("abc-defg"))
}

func TestURLValidator(t *testing.T) {
	validator := URLValidator()

	assert.NoError(t, validator("http://example.com"))
	assert.NoError(t, validator("https://example.com/path"))
	assert.Error(t, validator("not-a-url"))
}

func TestEmailValidator(t *testing.T) {
	validator := EmailValidator()

	assert.NoError(t, validator("test@example.com"))
	assert.NoError(t, validator("user.name+tag@example.co.uk"))
	assert.Error(t, validator("not-an-email"))
	assert.Error(t, validator("@example.com"))
}

func TestApplyDefaults(t *testing.T) {
	values := map[string]any{
		"server.port": 9090,
	}

	schema := ConfigSchema{
		Defaults: map[string]any{
			"server.port": 8080,
			"server.host": "localhost",
		},
	}

	result := ApplyDefaults(values, schema)

	assert.Equal(t, 9090, result["server.port"])        // 值覆盖默认
	assert.Equal(t, "localhost", result["server.host"]) // 使用默认
}

func TestToInt(t *testing.T) {
	tests := []struct {
		input    any
		expected int
		ok       bool
	}{
		{42, 42, true},
		{int64(42), 42, true},
		{float64(42), 42, true},
		{float64(42.5), 0, false},
		{uint(42), 42, true},
		{"42", 42, true},
		{" 42 ", 42, true},
		{"", 0, false},
		{nil, 0, false},
	}

	for _, tt := range tests {
		result, ok := toInt(tt.input)
		assert.Equal(t, tt.ok, ok)
		if ok {
			assert.Equal(t, tt.expected, result)
		}
	}
}

func TestLoadConfig_AppliesDefaultsForProviderBind(t *testing.T) {
	type testConfig struct {
		Server struct {
			Port int `config:"port"`
		} `config:"server"`
	}

	provider, err := NewProvider()
	require.NoError(t, err)

	schema := ConfigSchema{
		Defaults: map[string]any{
			"server.port": 8080,
		},
		Required: []string{"server.port"},
	}

	cfg, errs := LoadConfig[testConfig](provider, schema)
	require.NotNil(t, cfg)
	assert.Empty(t, errs)
	assert.Equal(t, 8080, cfg.Server.Port)
}
