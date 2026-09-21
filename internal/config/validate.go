package config

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ErrInvalid reports a resolved configuration a source made invalid.
var ErrInvalid = errors.New("config: invalid configuration")

// Validate checks the resolved configuration and reports every problem it
// finds, not just the first: a user fixing a config file should see all of the
// mistakes in one run. Each problem is wrapped with ErrInvalid, so a caller can
// match the class of failure with errors.Is.
func (c Config) Validate() error {
	var problems []error

	check := func(ok bool, format string, args ...any) {
		if !ok {
			problems = append(problems, fmt.Errorf(format, args...))
		}
	}

	// An unresolved directive is reported first: the key it names kept its
	// default, so every rule about that key would otherwise fire as well and
	// bury the one problem the user has to fix.
	for _, key := range slices.Sorted(maps.Keys(c.unresolved)) {
		problems = append(problems, fmt.Errorf("%s: %s is not set", key, c.unresolved[key]))
	}

	check(isOneOf(c.App.Env, EnvDevelopment, EnvStaging, EnvProduction, EnvTest),
		"app.env: %q is not one of %s", c.App.Env, joinValues(EnvDevelopment, EnvStaging, EnvProduction, EnvTest))
	check(c.App.DataDir != "", "app.data_dir: must not be empty")
	check(c.App.SecretKey == "" || isHexKey(c.App.SecretKey),
		"app.secret_key: must be 64 hex characters")

	check(isOneOf(c.Cache.Driver, CacheMemory, CacheValkey),
		"cache.driver: %q is not one of %s", c.Cache.Driver, joinValues(CacheMemory, CacheValkey))
	check(c.Cache.TTL > 0, "cache.ttl: must be positive")

	check(c.Database.URL != "", "database.url: must not be empty (set DATABASE_URL)")
	check(isPostgresDSN(c.Database.URL), "database.url: must be a postgres connection string")
	check(c.Database.MaxConns > 0, "database.max_conns: must be positive")
	check(c.Database.MinConns >= 0, "database.min_conns: must not be negative")
	check(c.Database.MinConns <= c.Database.MaxConns,
		"database.min_conns: %d must not exceed database.max_conns: %d",
		c.Database.MinConns, c.Database.MaxConns)
	check(c.Database.MaxConnLifetime > 0, "database.max_conn_lifetime: must be positive")
	check(c.Database.MaxConnIdleTime > 0, "database.max_conn_idle_time: must be positive")
	check(c.Database.ConnectTimeout > 0, "database.connect_timeout: must be positive")
	check(c.Database.SearchPath != "", "database.search_path: must not be empty")
	check(c.Database.Timezone != "", "database.timezone: must not be empty")

	check(isOneOf(c.Log.Level, LogDebug, LogInfo, LogWarn, LogError),
		"log.level: %q is not one of %s", c.Log.Level, joinValues(LogDebug, LogInfo, LogWarn, LogError))
	check(isOneOf(c.Log.Format, LogText, LogJSON),
		"log.format: %q is not one of %s", c.Log.Format, joinValues(LogText, LogJSON))

	check(isOneOf(c.RateLimit.Driver, RateLimitDB, RateLimitVK),
		"rate_limit.driver: %q is not one of %s", c.RateLimit.Driver, joinValues(RateLimitDB, RateLimitVK))
	check(c.RateLimit.Limit > 0, "rate_limit.limit: must be positive")
	check(c.RateLimit.Window > 0, "rate_limit.window: must be positive")

	check(c.Server.Host != "", "server.host: must not be empty")
	check(c.Server.Port > 0 && c.Server.Port <= 65535, "server.port: %d must be between 1 and 65535", c.Server.Port)
	check(c.Server.BaseURL == "" || isHTTPURL(c.Server.BaseURL),
		"server.base_url: %q must be an absolute http or https URL", c.Server.BaseURL)
	check(c.Server.ReadTimeout > 0, "server.read_timeout: must be positive")
	check(c.Server.WriteTimeout > 0, "server.write_timeout: must be positive")
	check(c.Server.IdleTimeout > 0, "server.idle_timeout: must be positive")
	check(c.Server.ShutdownTimeout > 0, "server.shutdown_timeout: must be positive")

	check(isOneOf(c.Session.Driver, SessionDB, SessionVK),
		"session.driver: %q is not one of %s", c.Session.Driver, joinValues(SessionDB, SessionVK))
	check(c.Session.TTL > 0, "session.ttl: must be positive")

	check(isOneOf(c.Storage.Driver, StorageLocal, StorageS3),
		"storage.driver: %q is not one of %s", c.Storage.Driver, joinValues(StorageLocal, StorageS3))
	check(c.Storage.LocalPath != "", "storage.local_path: must not be empty")

	// The key pair and the HMAC secret are alternatives: a token is signed with
	// one or the other, so at least one must be present.
	check(c.Auth.PrivateKey != "" || c.Auth.SecretKey != "",
		"auth: set auth.private_key or auth.secret_key")
	check(c.Auth.PrivateKey == "" || c.Auth.PublicKey != "",
		"auth.public_key: required when auth.private_key is set")
	check(c.Auth.Issuer != "", "auth.issuer: must not be empty")
	check(c.Auth.AccessTTL > 0, "auth.access_ttl: must be positive")
	check(c.Auth.RefreshTTL > 0, "auth.refresh_ttl: must be positive")
	check(c.Auth.RefreshTTL >= c.Auth.AccessTTL,
		"auth.refresh_ttl: must not be shorter than auth.access_ttl")

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n%s", ErrInvalid, indentProblems(problems))
}

// indentProblems renders one problem per line, each indented under the header.
// Every line is indented, not just the first: errors.Join separates them with a
// newline, so a single indent would leave the rest at column zero.
func indentProblems(problems []error) string {
	lines := make([]string, 0, len(problems))
	for _, problem := range problems {
		lines = append(lines, "  "+problem.Error())
	}
	return strings.Join(lines, "\n")
}

// isOneOf reports whether value matches one of the accepted values.
func isOneOf[T comparable](value T, accepted ...T) bool {
	for _, candidate := range accepted {
		if value == candidate {
			return true
		}
	}
	return false
}

// joinValues renders a value list for a validation message.
func joinValues(values ...string) string {
	return strings.Join(values, ", ")
}

// isHexKey reports whether value is a 64-character hex string, the encoding
// pkg/crypto expects for an AES-256 key.
func isHexKey(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := strconv.ParseUint(value[:16], 16, 64)
	if err != nil {
		return false
	}
	_, err = strconv.ParseUint(value[48:], 16, 64)
	return err == nil
}

// isPostgresDSN reports whether value parses as a Postgres connection string.
// The parse is pgx's, so a DSN that passes here connects the same way the
// datastore will read it.
func isPostgresDSN(value string) bool {
	_, err := pgx.ParseConfig(value)
	return err == nil
}

// isHTTPURL reports whether value is an absolute http or https URL.
func isHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Host != ""
}

// A caller validates a Config it did not write, and renders one safely when it
// prints it. Both act on a resolved Config, so both live here.

// redacted is the placeholder a Redacted Config prints instead of a secret.
const redacted = "[redacted]"

// Redacted returns a copy with every secret replaced by a placeholder, safe to
// print or log. The connection string is reduced to its host and database, so a
// report can name the target without leaking the password.
func (c Config) Redacted() Config {
	out := c
	out.App.SecretKey = redacted
	out.Auth.PrivateKey = redacted
	out.Auth.PublicKey = redacted
	out.Auth.SecretKey = redacted
	out.Database.URL = RedactDSN(c.Database.URL)
	out.origin = nil
	out.unresolved = nil
	return out
}

// RedactDSN reduces a Postgres connection string to host:port/database, the form
// the CLI prints for a database target. An unparsable string is replaced
// wholesale, because it may still carry a password.
func RedactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		return redacted
	}
	return fmt.Sprintf("%s:%d/%s", parsed.Host, parsed.Port, parsed.Database)
}

// String renders the configuration with every secret redacted, so an accidental
// %v of a Config cannot leak a credential.
func (c Config) String() string {
	redacted := c.Redacted()
	return fmt.Sprintf("app=%s database=%s server=%s:%d log=%s/%s",
		redacted.App.Env, RedactDSN(redacted.Database.URL), redacted.Server.Host,
		redacted.Server.Port, redacted.Log.Level, redacted.Log.Format)
}
