package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gochen/errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProvider_GetString 验证 Provider GetString。
func TestProvider_GetString(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"app.name": "test-app",
			"app.env":  "development",
		}),
	)
	require.NoError(t, err)

	assert.Equal(t, "test-app", provider.GetString("app.name", "default"))
	assert.Equal(t, "development", provider.GetString("app.env", "default"))
	assert.Equal(t, "default", provider.GetString("app.missing", "default"))
}

func TestProvider_GetString_TypeMismatch_UsesDefaultAndWarns(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"app.num": 123,
		}),
	)
	require.NoError(t, err)

	got := provider.GetString("app.num", "default")
	assert.Equal(t, "default", got)

	warnings := provider.Warnings()
	require.Len(t, warnings, 1)
	assert.Equal(t, "app.num", warnings[0].Key)
	assert.Equal(t, "string", warnings[0].Expected)
}

func TestProvider_GetStringStrict(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"app.name": "test-app",
			"app.num":  123,
		}),
	)
	require.NoError(t, err)

	got, err := provider.GetStringStrict("app.name")
	require.NoError(t, err)
	assert.Equal(t, "test-app", got)

	_, err = provider.GetStringStrict("app.missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))
	var appErr *errors.AppError
	if errors.As(err, &appErr) && appErr != nil {
		assert.Equal(t, "app.missing", appErr.Details()["key"])
	}

	_, err = provider.GetStringStrict("app.num")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))
}

// TestProvider_GetInt 验证 Provider GetInt。
func TestProvider_GetInt(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.port":    8080,
			"server.workers": " 4 ", // 字符串类型（包含空白）
		}),
	)
	require.NoError(t, err)

	assert.Equal(t, 8080, provider.GetInt("server.port", 3000))
	assert.Equal(t, 4, provider.GetInt("server.workers", 1))
	assert.Equal(t, 3000, provider.GetInt("server.missing", 3000))
}

func TestProvider_GetIntStrict(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.port":         8080,
			"server.workers":      "4",
			"server.bad":          "abc",
			"server.not_integral": 1.2,
		}),
	)
	require.NoError(t, err)

	port, err := provider.GetIntStrict("server.port")
	require.NoError(t, err)
	assert.Equal(t, 8080, port)

	workers, err := provider.GetIntStrict("server.workers")
	require.NoError(t, err)
	assert.Equal(t, 4, workers)

	_, err = provider.GetIntStrict("server.missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))

	_, err = provider.GetIntStrict("server.bad")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))

	_, err = provider.GetIntStrict("server.not_integral")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))
}

func TestProvider_GetBool(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"feature.enabled":  true,
			"feature.disabled": false,
			"feature.yes":      " yes ",
			"feature.no":       " no ",
		}),
	)
	require.NoError(t, err)

	assert.True(t, provider.GetBool("feature.enabled", false))
	assert.False(t, provider.GetBool("feature.disabled", true))
	assert.True(t, provider.GetBool("feature.yes", false))
	assert.False(t, provider.GetBool("feature.no", true))
	assert.True(t, provider.GetBool("feature.missing", true))
}

func TestProvider_GetBoolStrict(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"feature.enabled": true,
			"feature.yes":     "yes",
			"feature.bad":     "maybe",
			"feature.num":     1,
		}),
	)
	require.NoError(t, err)

	got, err := provider.GetBoolStrict("feature.enabled")
	require.NoError(t, err)
	assert.True(t, got)

	got, err = provider.GetBoolStrict("feature.yes")
	require.NoError(t, err)
	assert.True(t, got)

	got, err = provider.GetBoolStrict("feature.num")
	require.NoError(t, err)
	assert.True(t, got)

	_, err = provider.GetBoolStrict("feature.missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))

	_, err = provider.GetBoolStrict("feature.bad")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))
}

func TestProvider_GetDuration(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"timeout.request": " 30s ",
			"timeout.idle":    " 5m ",
			"timeout.connect": 10, // 秒数
		}),
	)
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, provider.GetDuration("timeout.request", time.Second))
	assert.Equal(t, 5*time.Minute, provider.GetDuration("timeout.idle", time.Second))
	assert.Equal(t, 10*time.Second, provider.GetDuration("timeout.connect", time.Second))
	assert.Equal(t, time.Minute, provider.GetDuration("timeout.missing", time.Minute))
}

func TestProvider_GetDurationStrict(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"timeout.request": "30s",
			"timeout.connect": 10,
			"timeout.bad":     "not-a-duration",
		}),
	)
	require.NoError(t, err)

	d, err := provider.GetDurationStrict("timeout.request")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, d)

	d, err = provider.GetDurationStrict("timeout.connect")
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, d)

	_, err = provider.GetDurationStrict("timeout.missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))

	_, err = provider.GetDurationStrict("timeout.bad")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))
}

func TestProvider_GetStringSlice(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"cors.origins": []string{"http://localhost", "http://example.com"},
			"cors.methods": "GET, POST,PUT, ",
		}),
	)
	require.NoError(t, err)

	origins := provider.GetStringSlice("cors.origins", nil)
	assert.Equal(t, []string{"http://localhost", "http://example.com"}, origins)

	methods := provider.GetStringSlice("cors.methods", nil)
	assert.Equal(t, []string{"GET", "POST", "PUT"}, methods)
}

// TestProvider_Bind 验证 Provider Bind。
func TestProvider_Bind(t *testing.T) {
	type ServerConfig struct {
		Port    int           `config:"port"`
		Host    string        `config:"host"`
		Timeout time.Duration `config:"timeout"`
	}

	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.port":    8080,
			"server.host":    "localhost",
			"server.timeout": "30s",
		}),
	)
	require.NoError(t, err)

	var cfg ServerConfig
	err = provider.Bind("server", &cfg)
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
}

// TestProvider_Priority 验证 Provider Priority。
func TestProvider_Priority(t *testing.T) {
	// 设置环境变量
	os.Setenv("TEST_APP_PORT", "9090")
	defer os.Unsetenv("TEST_APP_PORT")

	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"app.port": 8080,
		}),
		WithEnvSource("TEST_"),
	)
	require.NoError(t, err)

	// 环境变量应该覆盖默认值
	assert.Equal(t, 9090, provider.GetInt("app.port", 0))
}

// TestEnvSource 验证 EnvSource。
func TestEnvSource(t *testing.T) {
	os.Setenv("MYAPP_SERVER_PORT", "3000")
	os.Setenv("MYAPP_DATABASE_HOST", "localhost")
	defer func() {
		os.Unsetenv("MYAPP_SERVER_PORT")
		os.Unsetenv("MYAPP_DATABASE_HOST")
	}()

	source := NewEnvSource("MYAPP_")
	settings, err := source.Load()
	require.NoError(t, err)

	assert.Equal(t, "3000", settings["server.port"])
	assert.Equal(t, "localhost", settings["database.host"])
}

func TestEnvSource_File(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "db_password.txt")
	require.NoError(t, os.WriteFile(secretPath, []byte("s3cr3t\n"), 0600))

	os.Setenv("MYAPP_DATABASE_PASSWORD_FILE", secretPath)
	defer os.Unsetenv("MYAPP_DATABASE_PASSWORD_FILE")

	source := NewEnvSource("MYAPP_")
	settings, err := source.Load()
	require.NoError(t, err)

	assert.Equal(t, "s3cr3t", settings["database.password"])
}

func TestEnvSource_File_PreferExplicitValue(t *testing.T) {
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "db_password.txt")
	require.NoError(t, os.WriteFile(secretPath, []byte("from_file\n"), 0600))

	os.Setenv("MYAPP_DATABASE_PASSWORD", "from_env")
	os.Setenv("MYAPP_DATABASE_PASSWORD_FILE", secretPath)
	defer func() {
		os.Unsetenv("MYAPP_DATABASE_PASSWORD")
		os.Unsetenv("MYAPP_DATABASE_PASSWORD_FILE")
	}()

	source := NewEnvSource("MYAPP_")
	settings, err := source.Load()
	require.NoError(t, err)

	assert.Equal(t, "from_env", settings["database.password"])
}

// TestYAMLSource 验证 YAMLSource。
func TestYAMLSource(t *testing.T) {
	// 创建临时 YAML 文件
	content := `
server:
  port: 8080
  host: localhost
database:
  host: db.example.com
  port: 5432
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(content), 0644)
	require.NoError(t, err)

	source := NewYAMLSource(configPath)
	settings, err := source.Load()
	require.NoError(t, err)

	assert.Equal(t, 8080, settings["server.port"])
	assert.Equal(t, "localhost", settings["server.host"])
	assert.Equal(t, "db.example.com", settings["database.host"])
	assert.Equal(t, 5432, settings["database.port"])
}

// TestYAMLSource_Optional 验证 YAMLSource Optional。
func TestYAMLSource_Optional(t *testing.T) {
	source := NewYAMLSource("nonexistent.yaml", WithOptional())
	settings, err := source.Load()
	require.NoError(t, err)
	assert.Empty(t, settings)
}

// TestYAMLSource_NotFound 验证 YAMLSource NotFound。
func TestYAMLSource_NotFound(t *testing.T) {
	source := NewYAMLSource("nonexistent.yaml")
	_, err := source.Load()
	assert.Error(t, err)
}

// TestValidator_Required 验证 Validator Required。
func TestValidator_Required(t *testing.T) {
	provider, _ := NewProvider(
		WithDefaults(map[string]any{
			"server.port": 8080,
		}),
	)

	// 存在的配置
	validator := NewValidator().Required("server.port")
	err := validator.Validate(provider)
	assert.NoError(t, err)

	// 缺失的配置
	validator = NewValidator().Required("server.host")
	err = validator.Validate(provider)
	assert.Error(t, err)
}

func TestValidator_Required_TreatsNilAsMissing(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.port": nil,
		}),
	)
	require.NoError(t, err)

	validator := NewValidator().Required("server.port")
	err = validator.Validate(provider)
	assert.Error(t, err)
}

func TestValidator_Pattern_InvalidRegex_DoesNotPanic(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.host": "localhost",
		}),
	)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		validator := NewValidator().Pattern("server.host", "[")
		err := validator.Validate(provider)
		assert.Error(t, err)
	})
}

// TestValidator_Range 验证 Validator Range。
func TestValidator_Range(t *testing.T) {
	provider, _ := NewProvider(
		WithDefaults(map[string]any{
			"server.port": 8080,
		}),
	)

	// 在范围内
	validator := NewValidator().Range("server.port", 1, 65535)
	err := validator.Validate(provider)
	assert.NoError(t, err)

	// 超出范围
	validator = NewValidator().Range("server.port", 1, 1000)
	err = validator.Validate(provider)
	assert.Error(t, err)
}

// TestValidator_OneOf 验证 Validator OneOf。
func TestValidator_OneOf(t *testing.T) {
	provider, _ := NewProvider(
		WithDefaults(map[string]any{
			"log.level": "info",
		}),
	)

	// 在允许列表中
	validator := NewValidator().OneOf("log.level", "debug", "info", "warn", "error")
	err := validator.Validate(provider)
	assert.NoError(t, err)

	// 不在允许列表中
	validator = NewValidator().OneOf("log.level", "debug", "warn")
	err = validator.Validate(provider)
	assert.Error(t, err)
}

// TestValidator_MultipleErrors 验证 Validator MultipleErrors。
func TestValidator_MultipleErrors(t *testing.T) {
	provider, _ := NewProvider(
		WithDefaults(map[string]any{
			"server.port": 99999,
		}),
	)

	validator := NewValidator().
		Required("server.host").
		Range("server.port", 1, 65535)

	err := validator.Validate(provider)
	assert.Error(t, err)

	// 应该有两个错误
	validationErrors, ok := err.(ValidationErrors)
	assert.True(t, ok)
	assert.Len(t, validationErrors, 2)
}

func TestProvider_TolerantGet_ProducesWarnings(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.port": "not-an-int",
		}),
	)
	require.NoError(t, err)

	got := provider.GetInt("server.port", 8080)
	assert.Equal(t, 8080, got)

	warnings := provider.Warnings()
	require.Len(t, warnings, 1)
	assert.Equal(t, "server.port", warnings[0].Key)
}

func TestProvider_TolerantGet_WarningsBoundedByDefault(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.port": "not-an-int",
		}),
	)
	require.NoError(t, err)

	for i := 0; i < defaultWarningBufferSize+20; i++ {
		_ = provider.GetInt("server.port", 8080)
	}

	warnings := provider.Warnings()
	require.Len(t, warnings, defaultWarningBufferSize)
	assert.Equal(t, "server.port", warnings[len(warnings)-1].Key)
}

func TestProvider_TolerantGet_WarningsCanBeDisabled(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"server.port": "not-an-int",
		}),
		WithWarningBufferSize(0),
	)
	require.NoError(t, err)

	_ = provider.GetInt("server.port", 8080)
	warnings := provider.Warnings()
	require.Len(t, warnings, 0)
}

func TestProvider_AllSettings_RedactsSecrets(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"db.password": "supersecret",
			"db.user":     "alice",
		}),
	)
	require.NoError(t, err)

	all := provider.AllSettings()
	assert.Equal(t, "[REDACTED]", all["db.password"])
	assert.Equal(t, "alice", all["db.user"])
}

func TestProvider_StrictError_RedactsSecrets(t *testing.T) {
	provider, err := NewProvider(
		WithDefaults(map[string]any{
			"db.password": "supersecret",
		}),
	)
	require.NoError(t, err)

	// Force an invalid configuration error that would otherwise leak via:
	// - error context "value"
	// - wrapped strconv error message
	_, err = provider.GetIntStrict("db.password")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errors.Validation))

	var appErr *errors.AppError
	require.True(t, errors.As(err, &appErr) && appErr != nil)
	assert.Equal(t, "[REDACTED]", appErr.Details()["value"])
}
