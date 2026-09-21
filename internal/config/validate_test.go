package config_test

import (
	"strconv"
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

func TestMaskedKeepsTheEndsOfALongSecret(t *testing.T) {
	// The whole point of masking: two keys that look alike can be told apart,
	// while nothing usable is revealed.
	cfg := config.Default()
	cfg.App.SecretKey = secret

	assert.Equal(t, secret[:4]+"****"+secret[len(secret)-4:], cfg.Masked().App.SecretKey)
}

func TestMaskedHidesAShortSecretCompletely(t *testing.T) {
	// Keeping eight of a twelve-character password would leave most of it
	// readable, so a value below the floor is hidden in full.
	for _, password := range []string{"hunter2", "0123456789abcde"} {
		cfg := config.Default()
		cfg.Mailer.SMTPPassword = password

		assert.Equal(t, "[redacted]", cfg.Masked().Mailer.SMTPPassword, password)
	}

	cfg := config.Default()
	cfg.Mailer.SMTPPassword = "0123456789abcdef"
	assert.Equal(t, "0123****cdef", cfg.Masked().Mailer.SMTPPassword)
}

func TestMaskedLeavesAnEmptySecretEmpty(t *testing.T) {
	// An unset secret must read as unset, not as a masked value: the difference
	// is the whole answer when a key is missing.
	assert.Empty(t, config.Default().Masked().App.SecretKey)
	assert.Empty(t, config.Default().Masked().Auth.SecretKey)
}

func TestMaskedReducesTheDSNRatherThanMaskingIt(t *testing.T) {
	// A connection string is a composite value: revealing part of the string
	// says nothing, while the host is the part a reader needs.
	cfg := config.Default()
	cfg.Database.URL = "postgresql://user:sup3rs3cret@localhost:5432/tango?sslmode=disable"

	masked := cfg.Masked()

	assert.Equal(t, "localhost:5432/tango", masked.Database.URL)
	assert.NotContains(t, masked.Database.URL, "sup3rs3cret")
}

func TestMaskedAndRedactedAreDifferent(t *testing.T) {
	// Two renderings, two jobs. Masked is for a report the operator runs, and
	// shows enough to trace a value; Redacted is the fail-safe behind a log
	// line, and shows nothing at all.
	cfg := config.Default()
	cfg.App.SecretKey = secret

	assert.NotEqual(t, cfg.Masked().App.SecretKey, cfg.Redacted().App.SecretKey)
	assert.Equal(t, "[redacted]", cfg.Redacted().App.SecretKey)
	assert.NotContains(t, cfg.String(), secret[:4], "a log line must not show part of a key")
}

func TestValidationRejectsAKVDriverWithTheBackendOff(t *testing.T) {
	// The two settings are separate decisions, so they can disagree. A driver
	// pointing at a switched-off backend is the one combination that cannot
	// work, and it must be reported rather than fail at start-up.
	err := resolveFile(t, `"cache": {"driver": "kvstore"}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "kvstore.enable")
	assert.Contains(t, err.Error(), "cache.driver")
}

func TestValidationNamesEveryKVDriverThatDisagrees(t *testing.T) {
	err := resolveFile(t,
		`"cache": {"driver": "kvstore"}, `+
			`"rate_limit": {"driver": "kvstore"}, `+
			`"session": {"driver": "kvstore"}`)
	require.ErrorIs(t, err, config.ErrInvalid)

	// One message naming all three beats three messages naming one each.
	for _, key := range []string{"cache.driver", "rate_limit.driver", "session.driver"} {
		assert.Contains(t, err.Error(), key)
	}
}

func TestValidationAcceptsAKVDriverWithTheBackendOn(t *testing.T) {
	err := resolveFile(t, `"cache": {"driver": "kvstore"}, "kvstore": {"enable": true}`)
	assert.NoError(t, err)
}

func TestValidationAcceptsADisabledKVStore(t *testing.T) {
	// The default state: nothing points at the backend and it is switched off.
	require.NoError(t, resolveFile(t, ""))
}

func TestValidationChecksTheKVURLOnlyWhenEnabled(t *testing.T) {
	// A disabled backend is never dialled, so its URL is not held to anything.
	// Holding it would report a problem in a part of the file that is off.
	require.NoError(t, resolveFile(t, `"kvstore": {"enable": false, "url": "not-a-url"}`))

	err := resolveFile(t, `"kvstore": {"enable": true, "url": "not-a-url"}`)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "kvstore.url")
}

func TestValidationAcceptsTheURLGrammarTheClientAccepts(t *testing.T) {
	// Validation must not be stricter than the client, or it rejects a URL that
	// would have connected. These are the forms redis.ParseURL accepts: the
	// default port, an explicit port, TLS, a database index, a missing host
	// (defaulted to localhost:6379), and a unix socket.
	for _, rawURL := range []string{
		"redis://localhost",
		"redis://localhost:6379",
		"redis://default:securedb@localhost:6379",
		"rediss://cache.example.com:6380",
		"redis://localhost:6379/3",
		"redis://localhost:6379/0?dial_timeout=3&max_retries=2",
		"redis://",
		"unix:///var/run/valkey.sock",
	} {
		body := `"kvstore": {"enable": true, "url": ` + strconv.Quote(rawURL) + `}`
		assert.NoError(t, resolveFile(t, body), rawURL)
	}
}

func TestValidationRejectsAURLTheClientWouldReject(t *testing.T) {
	for _, rawURL := range []string{
		"http://localhost:6379",      // not a key-value scheme
		"redis://localhost:6379/a",   // the path must be a database number
		"redis://localhost:6379/1/2", // at most one segment
		"unix://",                    // a unix socket needs its path
		"redis://localhost:6379/%20", // an empty database number
	} {
		body := `"kvstore": {"enable": true, "url": ` + strconv.Quote(rawURL) + `}`
		assert.Error(t, resolveFile(t, body), rawURL)
	}
}

func TestRedactKVURLNamesTheServerWithoutThePassword(t *testing.T) {
	assert.Equal(t, "localhost:6379", config.RedactKVURL("redis://default:securedb@localhost:6379"))
	assert.Equal(t, "cache.example.com:6380/1", config.RedactKVURL("rediss://u:p@cache.example.com:6380/1"))
	assert.Equal(t, "localhost:6379", config.RedactKVURL("redis://"),
		"a missing host is what the client defaults, not a hidden URL")
	assert.Equal(t, "", config.RedactKVURL(""))
}

func TestRedactedHidesTheKVPassword(t *testing.T) {
	cfg := config.Default()
	cfg.KVStore.URL = "redis://default:sup3rs3cret@localhost:6379"

	for name, rendered := range map[string]config.Config{
		"Redacted": cfg.Redacted(),
		"Masked":   cfg.Masked(),
	} {
		assert.NotContains(t, rendered.KVStore.URL, "sup3rs3cret", name)
		assert.Contains(t, rendered.KVStore.URL, "localhost:6379", name)
	}
	assert.NotContains(t, cfg.String(), "sup3rs3cret")
}
