package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeEnvFile writes a dotenv file for the env-file layer tests.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env.test")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 3080, cfg.Port)
	assert.Equal(t, "development", cfg.App.Mode)
	assert.Equal(t, "structured", cfg.App.LogFormat)
	assert.Equal(t, "http://localhost:3000", cfg.Public.BaseURL)
	assert.NotEmpty(t, cfg.Database.URL)
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("APP_LOG_LEVEL", "debug")

	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, 9999, cfg.Port)
	assert.Equal(t, "debug", cfg.App.LogLevel)
}

func TestLoadEnvFileLayer(t *testing.T) {
	path := writeEnvFile(t, "PORT=1234\nAPP_LOG_LEVEL=debug\n")

	cfg, err := Load(LoadOptions{EnvFile: path})
	require.NoError(t, err)

	assert.Equal(t, 1234, cfg.Port)
	assert.Equal(t, "debug", cfg.App.LogLevel)
}

func TestLoadEnvFileOverridesSystemEnv(t *testing.T) {
	t.Setenv("PORT", "7777")
	path := writeEnvFile(t, "PORT=1234\nAPP_LOG_LEVEL=debug\n")

	cfg, err := Load(LoadOptions{EnvFile: path})
	require.NoError(t, err)

	assert.Equal(t, 1234, cfg.Port, "--env-file must win over the system environment")
	assert.Equal(t, "debug", cfg.App.LogLevel)
}

func TestLoadEnvFileMissing(t *testing.T) {
	_, err := Load(LoadOptions{EnvFile: "/nonexistent/tango/.env"})
	require.ErrorContains(t, err, "load env file")
}

func TestLoadOverridesWinOverEverything(t *testing.T) {
	t.Setenv("HOST", "from-env")
	path := writeEnvFile(t, "PORT=1234\n")

	cfg, err := Load(LoadOptions{
		EnvFile: path,
		Overrides: map[string]any{
			"host": "0.0.0.0",
			"port": 4321,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 4321, cfg.Port, "overrides must win over env file and env")
}

func TestLoadEmptyEnvIsUnset(t *testing.T) {
	t.Setenv("APP_MODE", "")

	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, "development", cfg.App.Mode, "empty env must not shadow the default")
}

func TestLoadTrustedOrigins(t *testing.T) {
	t.Setenv("PUBLIC_TRUSTED_ORIGINS", "http://a.test,http://b.test")

	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, []string{"http://a.test", "http://b.test"}, cfg.Public.TrustedOrigins)
}

func TestLoadNullPathPrefix(t *testing.T) {
	cfg, err := Load(LoadOptions{Overrides: map[string]any{
		"storage.s3_path_prefix": "null",
	}})
	require.NoError(t, err)
	assert.Nil(t, cfg.Storage.S3PathPrefix, "literal null must decode to nil")

	cfg, err = Load(LoadOptions{Overrides: map[string]any{
		"storage.s3_path_prefix": "tenant-a",
	}})
	require.NoError(t, err)
	require.NotNil(t, cfg.Storage.S3PathPrefix)
	assert.Equal(t, "tenant-a", *cfg.Storage.S3PathPrefix)
}

func TestLoadEnvFileIgnoresUnboundKeys(t *testing.T) {
	path := writeEnvFile(t, "HOME=/leaking\nPORT=4400\n")

	cfg, err := Load(LoadOptions{EnvFile: path})
	require.NoError(t, err)

	assert.Equal(t, 4400, cfg.Port)
	assert.Equal(t, "localhost", cfg.Host, "unbound keys must not leak into config")
}

func TestLoadEnvFileRejectsUnknownKeys(t *testing.T) {
	path := writeEnvFile(t, "AUTH_ACCESS_TOKEN_EXPIRED=900\n")

	_, err := Load(LoadOptions{EnvFile: path})
	require.ErrorContains(t, err, "unknown config key", "typos must fail fast, not silently default")
}

func TestValidateProductionRequiresSecrets(t *testing.T) {
	base := LoadOptions{Overrides: map[string]any{
		"app.mode":               "production",
		"database.url":           "postgresql://localhost/db",
		"public.healthcheck_url": "https://upstream.test",
	}}

	// Defaults leave the secrets empty: production must refuse.
	_, err := Load(base)
	require.Error(t, err)
	for _, key := range []string{"app.secret_key", "auth.secret_key", "auth.private_key", "auth.public_key"} {
		assert.Contains(t, err.Error(), key)
	}

	// With all four secrets set it validates cleanly.
	base.Overrides["app.secret_key"] = "s3cret"
	base.Overrides["auth.secret_key"] = "s3cret"
	base.Overrides["auth.private_key"] = "priv"
	base.Overrides["auth.public_key"] = "pub"
	_, err = Load(base)
	require.NoError(t, err)
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name      string
		overrides map[string]any
		wantErr   string
	}{
		{
			name:      "port too high",
			overrides: map[string]any{"port": 70000},
			wantErr:   "port: 70000 out of range",
		},
		{
			name:      "bad mode",
			overrides: map[string]any{"app.mode": "prodaksen"},
			wantErr:   `app.mode: invalid value "prodaksen"`,
		},
		{
			name:      "bad log format",
			overrides: map[string]any{"app.log_format": "xml"},
			wantErr:   `app.log_format: invalid value "xml"`,
		},
		{
			name:      "bad log transport",
			overrides: map[string]any{"app.log_transport": "syslog"},
			wantErr:   `app.log_transport: invalid value "syslog"`,
		},
		{
			name:      "bad log level",
			overrides: map[string]any{"app.log_level": "loud"},
			wantErr:   `app.log_level: invalid value "loud"`,
		},
		{
			name:      "bad database scheme",
			overrides: map[string]any{"database.url": "mysql://localhost/db"},
			wantErr:   `database.url: unsupported scheme "mysql"`,
		},
		{
			name:      "bad base url scheme",
			overrides: map[string]any{"public.base_url": "ftp://localhost"},
			wantErr:   `public.base_url: unsupported scheme "ftp"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(LoadOptions{Overrides: tc.overrides})
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestDefaultsAreComplete(t *testing.T) {
	// Every leaf without an intentional default must be non-zero in
	// defaultConfig — the struct literal is the single source of
	// truth, this locks it.
	intentionallyEmpty := map[string]bool{
		"app.secret_key":               true,
		"auth.private_key":             true,
		"auth.public_key":              true,
		"auth.secret_key":              true,
		"auth.github_client_id":        true,
		"auth.github_client_secret":    true,
		"auth.google_client_id":        true,
		"auth.google_client_secret":    true,
		"mailer.smtp_username":         true,
		"mailer.smtp_password":         true,
		"public.trusted_origins":       true, // nil = no extra origins
		"storage.s3_access_key_id":     true, // credentials stay out of code
		"storage.s3_secret_access_key": true,
		"storage.s3_path_prefix":       true, // nil = no prefix
	}

	var walk func(value reflect.Value, prefix string)
	walk = func(value reflect.Value, prefix string) {
		structType := value.Type()
		for i := range structType.NumField() {
			field := structType.Field(i)
			name := field.Tag.Get("koanf")
			if name == "" {
				continue
			}
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}

			fieldValue := value.Field(i)
			if field.Type.Kind() == reflect.Struct {
				walk(fieldValue, path)
				continue
			}
			if intentionallyEmpty[path] || field.Type.Kind() == reflect.Bool {
				// Bool false is a legitimate default.
				continue
			}
			assert.False(t, fieldValue.IsZero(), "defaultConfig.%s must have a default value", path)
		}
	}
	walk(reflect.ValueOf(defaultConfig), "")
}

func TestEnvExampleInSync(t *testing.T) {
	// .env.example documents every bound key; this test fails when
	// the struct and the example drift apart, in either direction.
	data, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	documented := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, found := strings.Cut(line, "=")
		require.True(t, found, "malformed line in .env.example: %q", line)

		key, _ := envTransform(name, "probe")
		require.NotEmpty(t, key, "env %q in .env.example is not a bound key", name)
		require.True(t, validKeys[key], "env %q maps to unknown key %q", name, key)
		documented[key] = true
	}

	for key := range validKeys {
		assert.True(t, documented[key], "key %q is missing from .env.example", key)
	}
}
