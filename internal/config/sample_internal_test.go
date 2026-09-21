package config

import (
	"os"
	"path/filepath"
	"slices"
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
	probeKVURL  = "redis://default:securedb@localhost:6379"
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
	// A secret is never written literally. Its variable name comes from envKeys
	// when one names it, and from EnvName otherwise.
	flat := sampleDoc(t)

	for _, key := range secretKeys {
		name, named := envKeys[key]
		if !named {
			name = EnvName(key)
		}
		assert.Equal(t, "env:"+name, flat[key],
			"%s must be written as a directive, never as a literal", key)
	}
}

func TestSampleWritesDeploymentKeysAsDirectives(t *testing.T) {
	// A deployment sets the mode and the public origin, so the file asks for the
	// variable instead of baking in a value that would be wrong there. The name
	// is the one envKeys writes, which is not always the key upper-cased.
	flat := sampleDoc(t)

	for key, name := range envKeys {
		assert.Equal(t, "env:"+name, flat[key], "%s must name %s", key, name)
	}
	assert.Equal(t, "env:PUBLIC_BASE_URL", flat["server.base_url"],
		"the public origin is PUBLIC_BASE_URL, not SERVER_BASE_URL")
	assert.Equal(t, "env:VALKEY_URL", flat["kvstore.url"],
		"the key-value URL is VALKEY_URL, not KVSTORE_URL")
}

func TestDeploymentKeysThatAreNotSecrets(t *testing.T) {
	// A key in envKeys is not automatically a secret, and the reverse holds too.
	// Redacted hides secrets, and hiding a runtime mode would make a report
	// harder to read for no gain.
	//
	// A key in both lists is a secret whose variable name is pinned rather than
	// derived, so a rename of the key cannot silently rename the variable a
	// deployment sets. This asserts the overlap is exactly that one key, so a
	// second one is a decision rather than an accident.
	//
	// The S3 secrets are not here: their variable names are what EnvName
	// derives, so listing them would restate a rule rather than pin a name.
	var both []string
	for key := range envKeys {
		if slices.Contains(secretKeys, key) {
			both = append(both, key)
		}
	}
	slices.Sort(both)

	assert.Equal(t, []string{"kvstore.url"}, both)
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
			"MAILER_SMTP_USERNAME=bot",
			"MAILER_SMTP_PASSWORD=" + probeSecret,
		},
	})
	require.NoError(t, err)

	defaults := Default()
	assert.Equal(t, defaults.Server, cfg.Server)
	assert.Equal(t, defaults.Database.MaxConns, cfg.Database.MaxConns)
	assert.Equal(t, defaults.Auth.AccessTTL, cfg.Auth.AccessTTL)
	assert.Equal(t, probeDSN, cfg.Database.URL)
	assert.Equal(t, probeSecret, cfg.Auth.SecretKey)
	assert.Equal(t, probeSecret, cfg.Mailer.SMTPPassword)
	assert.NoError(t, cfg.Validate())
}

func TestSampleMailerDirectivesResolveFromTheEnvironment(t *testing.T) {
	// The mailer keys are written as directives naming the conventional
	// variables, so a deployment fills them without editing the file. The port
	// and the TLS flag arrive as strings and must decode to int and bool.
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
			"MAILER_SMTP_HOST=smtp.example.com",
			"MAILER_SMTP_PORT=465",
			"MAILER_SMTP_USERNAME=bot",
			"MAILER_SMTP_PASSWORD=" + probeSecret,
			"MAILER_SMTP_SECURE=true",
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "smtp.example.com", cfg.Mailer.SMTPHost)
	assert.Equal(t, 465, cfg.Mailer.SMTPPort)
	assert.Equal(t, "bot", cfg.Mailer.SMTPUsername)
	assert.Equal(t, probeSecret, cfg.Mailer.SMTPPassword)
	assert.True(t, cfg.Mailer.SMTPSecure)
	assert.NoError(t, cfg.Validate())
}

func TestEverySampleSecretIsRendered(t *testing.T) {
	// The list and the renderings must agree. If a key Sample writes as a
	// directive were not a key these render, a generated file would carry a
	// literal credential or a secret could reach a report or a log line.
	//
	// Both renderings are checked, not just Redacted: Masked is what
	// config:print uses, so a secret added to the list and forgotten there would
	// print in full.
	cfg := Default()
	cfg.App.SecretKey = probeSecret
	cfg.Auth.PrivateKey = probeSecret
	cfg.Auth.PublicKey = probeSecret
	cfg.Auth.SecretKey = probeSecret
	cfg.Database.URL = probeDSN
	cfg.KVStore.URL = probeKVURL
	cfg.Mailer.SMTPPassword = probeSecret
	cfg.Storage.S3.AccessKeyID = probeSecret
	cfg.Storage.S3.AccessKeySecret = probeSecret

	for name, rendered := range map[string]Config{
		"Redacted": cfg.Redacted(),
		"Masked":   cfg.Masked(),
	} {
		for _, key := range secretKeys {
			assert.NotEqual(t, probeSecret, valueAt(rendered, key), "%s must be hidden by %s", key, name)
			assert.NotEqual(t, probeDSN, valueAt(rendered, key), "%s must be hidden by %s", key, name)
		}
	}
}

func TestMaskedShowsNothingOfAShortSecret(t *testing.T) {
	// The two renderings differ in how much they show, so the assertion above
	// cannot catch a Masked that returned the value unchanged for a short one.
	cfg := Default()
	cfg.Mailer.SMTPPassword = "hunter2"

	assert.NotContains(t, cfg.Masked().Mailer.SMTPPassword, "hunter")
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
	case "kvstore.url":
		return cfg.KVStore.URL
	case "mailer.smtp_password":
		return cfg.Mailer.SMTPPassword
	case "storage.s3.access_key_id":
		return cfg.Storage.S3.AccessKeyID
	case "storage.s3.access_key_secret":
		return cfg.Storage.S3.AccessKeySecret
	default:
		return ""
	}
}

func TestValuesRenderDurationsAsSeconds(t *testing.T) {
	// A printed value must be comparable against the config file, which writes
	// durations as seconds. Anything else would make the report read differently
	// from the file it describes.
	cfg := Default()
	values := Values(cfg)

	assert.Equal(t, int64(900), values["auth.access_ttl"])
	assert.Equal(t, int64(3600), values["database.max_conn_lifetime"])
	assert.Equal(t, "storage", values["storage.local_path"])
	assert.Equal(t, true, values["mailer.smtp_secure"] != nil)
}

func TestValuesCoverEveryKey(t *testing.T) {
	values := Values(Default())

	for _, key := range Keys() {
		assert.Contains(t, values, key, "%s must have a value", key)
	}
	assert.Len(t, values, len(Keys()))
}

func TestSampleWritesTheS3KeysAsDirectives(t *testing.T) {
	// Every S3 key a deployment sets is written as a directive naming its
	// conventional variable, so the file carries no credential and a deployment
	// fills the section without editing it. The two secrets are covered by the
	// secret test; this is the rest of the section.
	flat := sampleDoc(t)

	assert.Equal(t, "env:STORAGE_S3_BUCKET_NAME", flat["storage.s3.bucket_name"])
	assert.Equal(t, "env:STORAGE_S3_ENDPOINT_URL", flat["storage.s3.endpoint_url"])
	assert.Equal(t, "env:STORAGE_S3_REGION", flat["storage.s3.region"])
}

func TestSampleWritesThePathPrefixAsNull(t *testing.T) {
	// An empty prefix means "no prefix", which a null says more directly than an
	// empty string. The two resolve alike, so this is about what the file reads
	// like: the key is present, so it is discoverable, and its value says there
	// is nothing to set.
	raw, err := Sample()
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))

	storage, ok := doc["storage"].(map[string]any)
	require.True(t, ok)
	s3, ok := storage["s3"].(map[string]any)
	require.True(t, ok)

	value, present := s3["path_prefix"]
	assert.True(t, present, "the key must be discoverable in the file")
	assert.Nil(t, value, "an empty prefix is written as null, not as an empty string")

	// The null must not disturb the keys around it.
	assert.Equal(t, true, s3["force_path_style"])
	assert.Equal(t, float64(3600), s3["signed_url_expires"])
}

func TestNullKeysArePartOfTheSchema(t *testing.T) {
	// A key written as null must be a real config key, or the generated file
	// would carry an entry the loader drops.
	for _, key := range nullKeys {
		assert.Contains(t, Keys(), key)
	}
}

func TestSampleS3SectionResolvesToTheDefaults(t *testing.T) {
	// The strongest statement about the generated S3 section: loading it back
	// with the variables set yields the built-in defaults.
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
			"STORAGE_S3_BUCKET_NAME=devbucket",
			"STORAGE_S3_ENDPOINT_URL=http://localhost:9100",
			"STORAGE_S3_REGION=us-east-1",
			"STORAGE_S3_ACCESS_KEY_ID=s3admin",
			"STORAGE_S3_ACCESS_KEY_SECRET=s3passw0rd",
		},
	})
	require.NoError(t, err)

	defaults := Default()
	assert.Equal(t, defaults.Storage.S3.ForcePathStyle, cfg.Storage.S3.ForcePathStyle)
	assert.Equal(t, defaults.Storage.S3.SignedURLExpires, cfg.Storage.S3.SignedURLExpires)
	assert.Equal(t, "", cfg.Storage.S3.PathPrefix, "a null leaves the default")

	assert.Equal(t, "devbucket", cfg.Storage.S3.BucketName)
	assert.Equal(t, "http://localhost:9100", cfg.Storage.S3.EndpointURL)
	assert.Equal(t, "us-east-1", cfg.Storage.S3.Region)
	assert.Equal(t, "s3admin", cfg.Storage.S3.AccessKeyID)
	assert.Equal(t, "s3passw0rd", cfg.Storage.S3.AccessKeySecret)
}
