package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

// resolveAndValidate is the pair a caller that needs the whole configuration
// uses: Load merges the sources, Validate checks the result.
func resolveAndValidate(t *testing.T, opts config.Options) (config.Config, error) {
	t.Helper()

	cfg, err := config.Load(opts)
	if err != nil {
		return config.Config{}, err
	}
	return cfg, cfg.Validate()
}

// resolveFile validates the configuration a file body resolves to.
func resolveFile(t *testing.T, extra string) error {
	t.Helper()

	_, err := resolveAndValidate(t, config.Options{
		ConfigFile: configFile(t, extra),
		Environ:    baseEnv(),
	})
	return err
}

func TestValidationReportsEveryProblem(t *testing.T) {
	// A missing DSN and an impossible port: both must be reported at once, so a
	// file with several mistakes is fixed in one pass.
	_, err := resolveAndValidate(t, config.Options{
		ConfigFile: writeConfig(t, `{"auth": {"secret_key": "env:AUTH_SECRET_KEY"}, "server": {"port": 0}}`),
		Environ:    []string{"AUTH_SECRET_KEY=" + secret},
	})
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "database.url")
	assert.Contains(t, err.Error(), "server.port")
}

func TestValidationRejectsBadDriver(t *testing.T) {
	err := resolveFile(t, `"cache": {"driver": "bogus"}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "cache.driver")
}

func TestValidationRequiresASigningKey(t *testing.T) {
	// The key pair and the HMAC secret are alternatives; neither present means
	// no token could be signed.
	_, err := resolveAndValidate(t, config.Options{
		ConfigFile: writeConfig(t, `{"database": {"url": "env:DATABASE_URL"}}`),
		Environ:    []string{"DATABASE_URL=" + dsn},
	})
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "auth.private_key or auth.secret_key")
}

func TestValidationRequiresPublicKeyWithPrivateKey(t *testing.T) {
	err := resolveFile(t, `"auth": {"private_key": "abc"}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "auth.public_key")
}

func TestValidationRejectsMinAboveMax(t *testing.T) {
	err := resolveFile(t, `"database": {"min_conns": 20, "max_conns": 5}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "database.min_conns")
}

func TestValidationAcceptsAValidConfiguration(t *testing.T) {
	require.NoError(t, resolveFile(t, ""))
}

func TestValidationRejectsAnInvalidModeName(t *testing.T) {
	err := resolveFile(t, `"app": {"mode": "prod"}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "app.mode")
}

// The mailer is optional, so a configuration that names no SMTP host is valid:
// that is what lets a local checkout run without a mail server.
func TestValidationAcceptsAnUnconfiguredMailer(t *testing.T) {
	cfg, err := resolveAndValidate(t, config.Options{
		ConfigFile: configFile(t, ""),
		Environ:    baseEnv(),
	})
	require.NoError(t, err)

	assert.Empty(t, cfg.Mailer.SMTPHost)
	assert.Equal(t, "mailer@example.com", cfg.Mailer.FromEmail)
	assert.Equal(t, "Tango Mailer", cfg.Mailer.FromName)
	assert.Equal(t, 587, cfg.Mailer.SMTPPort)
}

func TestValidationRejectsAnInvalidSenderAddress(t *testing.T) {
	err := resolveFile(t, `"mailer": {"from_email": "not-an-address"}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "mailer.from_email")
}

func TestValidationRejectsAPasswordWithoutAUsername(t *testing.T) {
	// A password that authenticates nothing is a mistake, not a setting.
	err := resolveFile(t, `"mailer": {"smtp_password": "hunter2"}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "mailer.smtp_username")
}

func TestRedactedHidesTheSMTPPassword(t *testing.T) {
	cfg, err := resolveAndValidate(t, config.Options{
		ConfigFile: configFile(t, `"mailer": {"smtp_username": "bot", "smtp_password": "hunter2"}`),
		Environ:    baseEnv(),
	})
	require.NoError(t, err)

	redacted := cfg.Redacted()

	assert.Equal(t, "[redacted]", redacted.Mailer.SMTPPassword)
	assert.Equal(t, "bot", redacted.Mailer.SMTPUsername, "a username is not a secret")
	assert.NotContains(t, redacted.String(), "hunter2")
}

func TestRedactedHidesSecrets(t *testing.T) {
	cfg, err := resolveAndValidate(t, config.Options{
		ConfigFile: configFile(t, ""),
		Environ:    baseEnv(),
	})
	require.NoError(t, err)

	redacted := cfg.Redacted()

	assert.Equal(t, "[redacted]", redacted.Auth.SecretKey)
	assert.NotContains(t, redacted.Database.URL, "pass")
	assert.Contains(t, redacted.Database.URL, "localhost:5432/tango")
	assert.NotContains(t, cfg.String(), "pass", "String must not leak the password")
}

func TestRedactedLeavesNothingBehind(t *testing.T) {
	cfg, err := resolveAndValidate(t, config.Options{
		ConfigFile: configFile(t, `"app": {"secret_key": "env:APP_SECRET_KEY"}`),
		Environ:    append(baseEnv(), "APP_SECRET_KEY="+secret),
	})
	require.NoError(t, err)

	redacted := cfg.Redacted()

	assert.Empty(t, redacted.Origin("database.url"), "Origins must be dropped with the secrets")
	for _, secret := range []string{cfg.App.SecretKey, cfg.Auth.SecretKey, cfg.Database.URL} {
		assert.NotContains(t, redacted.String(), secret)
	}
}

func TestKeysMatchTheStruct(t *testing.T) {
	keys := config.Keys()
	require.NotEmpty(t, keys)

	assert.Contains(t, keys, "storage.local_path")
	assert.Contains(t, keys, "app.mode")
	assert.Contains(t, keys, "database.url")
	assert.Contains(t, keys, "server.port")
	assert.Contains(t, keys, "auth.access_ttl")
	assert.IsIncreasing(t, keys, "Keys must be sorted")
}
