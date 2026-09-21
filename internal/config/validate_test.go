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

func TestValidationReportsEveryProblem(t *testing.T) {
	// No DATABASE_URL and an impossible port: both must be reported at once.
	_, err := resolveAndValidate(t, config.Options{
		Environ: []string{"AUTH_SECRET_KEY=" + secret, "SERVER_PORT=0"},
	})
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "database.url")
	assert.Contains(t, err.Error(), "server.port")
}

func TestValidationRejectsBadDriver(t *testing.T) {
	_, err := resolveAndValidate(t, config.Options{
		Environ: append(baseEnv(), "CACHE_DRIVER=bogus"),
	})
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "cache.driver")
}

func TestValidationRequiresASigningKey(t *testing.T) {
	// The key pair and the HMAC secret are alternatives; neither present means
	// no token could be signed.
	_, err := resolveAndValidate(t, config.Options{Environ: []string{"DATABASE_URL=" + dsn}})
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "auth.private_key or auth.secret_key")
}

func TestValidationRequiresPublicKeyWithPrivateKey(t *testing.T) {
	_, err := resolveAndValidate(t, config.Options{
		Environ: append(baseEnv(), "AUTH_PRIVATE_KEY=abc"),
	})
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "auth.public_key")
}

func TestValidationRejectsMinAboveMax(t *testing.T) {
	_, err := resolveAndValidate(t, config.Options{
		Environ: append(baseEnv(), "DATABASE_MIN_CONNS=20", "DATABASE_MAX_CONNS=5"),
	})
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "database.min_conns")
}

func TestValidationAcceptsAValidConfiguration(t *testing.T) {
	_, err := resolveAndValidate(t, config.Options{Environ: baseEnv()})
	require.NoError(t, err)
}

func TestRedactedHidesSecrets(t *testing.T) {
	cfg, err := resolveAndValidate(t, config.Options{Environ: baseEnv()})
	require.NoError(t, err)

	redacted := cfg.Redacted()

	assert.Equal(t, "[redacted]", redacted.Auth.SecretKey)
	assert.NotContains(t, redacted.Database.URL, "pass")
	assert.Contains(t, redacted.Database.URL, "localhost:5432/tango")
	assert.NotContains(t, cfg.String(), "pass", "String must not leak the password")
}

func TestRedactedLeavesNothingBehind(t *testing.T) {
	cfg, err := resolveAndValidate(t, config.Options{
		Environ: append(baseEnv(), "APP_SECRET_KEY="+secret, "AUTH_PRIVATE_KEY=pk", "AUTH_PUBLIC_KEY=pub"),
	})
	require.NoError(t, err)

	redacted := cfg.Redacted()

	assert.Empty(t, redacted.Origin("database.url"), "Origins must be dropped with the secrets")
	for _, secret := range []string{cfg.App.SecretKey, cfg.Auth.SecretKey, cfg.Auth.PrivateKey, "pass"} {
		assert.NotContains(t, redacted.String(), secret)
	}
}

func TestKeysMatchTheStruct(t *testing.T) {
	keys := config.Keys()
	require.NotEmpty(t, keys)

	assert.Contains(t, keys, "app.data_dir")
	assert.Contains(t, keys, "database.url")
	assert.Contains(t, keys, "server.port")
	assert.Contains(t, keys, "auth.access_ttl")
	assert.IsIncreasing(t, keys, "Keys must be sorted")
}
