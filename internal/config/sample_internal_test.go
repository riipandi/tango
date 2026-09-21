package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"encoding/json/jsontext"
	"encoding/json/v2"
)

// A local copy of the two fixtures, because this file is in package config and
// cannot see the external test package's helpers.
const (
	probeDSN    = "postgresql://user:pass@localhost:5432/tango?sslmode=disable"
	probeSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func sampleDoc(t *testing.T) map[string]any {
	t.Helper()

	raw, err := Sample()
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return flattenNested(doc, nil)
}

func TestSampleWritesEverySecretAsADirective(t *testing.T) {
	flat := sampleDoc(t)

	for _, key := range secretKeys {
		assert.Equal(t, "env:"+EnvName(key), flat[key],
			"%s must be written as a directive, never as a literal", key)
	}
}

func TestSampleCoversEveryKey(t *testing.T) {
	// A generated file is also the list of what can be configured, so it must
	// carry every key the struct defines.
	flat := sampleDoc(t)

	require.Len(t, flat, len(Keys()))
	for _, key := range Keys() {
		assert.Contains(t, flat, key)
	}
}

func TestSampleIsDeterministic(t *testing.T) {
	first, err := Sample()
	require.NoError(t, err)
	second, err := Sample()
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second), "two runs must produce the same bytes")
	assert.True(t, jsontext.Value(first).IsValid(), "the sample must be valid JSON")
}

func TestSampleWritesDurationsAsSeconds(t *testing.T) {
	// A duration is written as a plain number of seconds, never as a Go duration
	// string: 900 reads as a duration, "15m0s" reads as an expression.
	flat := sampleDoc(t)

	assert.Equal(t, float64(900), flat["auth.access_ttl"])
	assert.Equal(t, float64(3600), flat["database.max_conn_lifetime"])
	assert.Equal(t, float64(2592000), flat["auth.refresh_ttl"])
}

func TestDurationKeysMatchTheStruct(t *testing.T) {
	// The list is what makes a bare number mean seconds, so a new duration field
	// must be listed or it would silently be read as nanoseconds.
	var found []string
	for _, key := range Keys() {
		if _, ok := DefaultsMap()[key].(time.Duration); ok {
			found = append(found, key)
		}
	}

	assert.Equal(t, durationKeys, found,
		"durationKeys must list exactly the time.Duration fields on Config")
}

func TestSampleRoundTripsThroughLoad(t *testing.T) {
	// The strongest statement about the generated file: loading it back yields
	// the built-in defaults, with the directives resolved.
	raw, err := Sample()
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "app.config.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	cfg, err := Load(Options{
		ConfigFile: path,
		Environ: []string{
			"DATABASE_URL=" + probeDSN,
			"AUTH_SECRET_KEY=" + probeSecret,
			"APP_SECRET_KEY=" + probeSecret,
			"AUTH_PRIVATE_KEY=" + probeSecret,
			"AUTH_PUBLIC_KEY=" + probeSecret,
		},
	})
	require.NoError(t, err)

	defaults := Default()
	assert.Equal(t, defaults.Server, cfg.Server)
	assert.Equal(t, defaults.Database.MaxConns, cfg.Database.MaxConns)
	assert.Equal(t, defaults.Auth.AccessTTL, cfg.Auth.AccessTTL)
	assert.Equal(t, probeDSN, cfg.Database.URL)
	assert.Equal(t, probeSecret, cfg.Auth.SecretKey)
	assert.NoError(t, cfg.Validate())
}

func TestEverySampleSecretIsRedacted(t *testing.T) {
	// The two lists must agree. If a key Sample writes as a directive were not a
	// key Redacted hides, a generated file would carry a literal credential or a
	// secret could reach a log line.
	cfg := Default()
	cfg.App.SecretKey = probeSecret
	cfg.Auth.PrivateKey = probeSecret
	cfg.Auth.PublicKey = probeSecret
	cfg.Auth.SecretKey = probeSecret
	cfg.Database.URL = probeDSN

	redacted := cfg.Redacted()
	for _, key := range secretKeys {
		assert.NotEqual(t, probeSecret, valueAt(redacted, key), "%s must be redacted", key)
		assert.NotEqual(t, probeDSN, valueAt(redacted, key), "%s must be redacted", key)
	}
}

// valueAt reads a config key back out of a Config, so the redaction assertion
// covers every key in the list without naming each field.
func valueAt(cfg Config, key string) string {
	switch key {
	case "app.secret_key":
		return cfg.App.SecretKey
	case "auth.private_key":
		return cfg.Auth.PrivateKey
	case "auth.public_key":
		return cfg.Auth.PublicKey
	case "auth.secret_key":
		return cfg.Auth.SecretKey
	case "database.url":
		return cfg.Database.URL
	default:
		return ""
	}
}
