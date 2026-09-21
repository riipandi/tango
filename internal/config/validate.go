package config

import (
	"errors"
	"fmt"
	"net/mail"
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

	check(isOneOf(c.App.Mode, ModeDevelopment, ModeStaging, ModeProduction, ModeTest),
		"app.mode: %q is not one of %s", c.App.Mode,
		joinValues(ModeDevelopment, ModeStaging, ModeProduction, ModeTest))
	check(c.App.SecretKey == "" || isHexKey(c.App.SecretKey),
		"app.secret_key: must be 64 hex characters")

	check(isOneOf(c.Cache.Driver, CacheMemory, CacheKV),
		"cache.driver: %q is not one of %s", c.Cache.Driver, joinValues(CacheMemory, CacheKV))
	check(c.Cache.TTL > 0, "cache.ttl: must be positive")

	check(c.Database.URL != "", "database.url: %s",
		c.unsetNote("database.url", "must not be empty (set DATABASE_URL)"))
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
	check(isOneOf(c.Log.Format, LogPretty, LogStructured),
		"log.format: %q is not one of %s", c.Log.Format, joinValues(LogPretty, LogStructured))

	// The key-value backend is opt-in. Its Enable flag and the per-feature driver
	// fields are two different decisions, so they can disagree, and a driver
	// pointing at a backend that is switched off is the one combination that
	// cannot work: report it by name rather than leaving it to fail at start-up.
	check(c.KVStore.DB >= 0, "kvstore.db: %d must not be negative", c.KVStore.DB)
	if c.KVStore.Enable {
		// An unset variable is reported by name, which is the message a user
		// needs; a URL that is present is held to the grammar the client accepts.
		check(c.KVStore.URL != "", "kvstore.url: %s",
			c.unsetNote("kvstore.url", "must not be empty when kvstore.enable is true"))
		check(c.KVStore.URL == "" || isKVURL(c.KVStore.URL),
			"kvstore.url: %q must be a redis://, rediss://, or unix:// connection string", c.KVStore.URL)
	}
	if drivers := c.kvStoreDrivers(); len(drivers) > 0 && !c.KVStore.Enable {
		problems = append(problems, fmt.Errorf(
			"kvstore.enable: false, but %s set to %q; enable the kvstore or choose another driver",
			strings.Join(drivers, ", "), CacheKV))
	}

	// The mailer is optional: with no SMTP host the application runs, it just
	// cannot send mail, so an empty host is not a problem. What is checked is
	// that a host which is set has a usable port, and that a password is not
	// given without the username it authenticates.
	check(isEmail(c.Mailer.FromEmail), "mailer.from_email: %q must be an email address", c.Mailer.FromEmail)
	check(c.Mailer.FromName != "", "mailer.from_name: must not be empty")
	check(c.Mailer.SMTPPort > 0 && c.Mailer.SMTPPort <= 65535,
		"mailer.smtp_port: %d must be between 1 and 65535", c.Mailer.SMTPPort)
	check(c.Mailer.SMTPPassword == "" || c.Mailer.SMTPUsername != "",
		"mailer.smtp_username: required when mailer.smtp_password is set")

	check(isOneOf(c.RateLimit.Driver, RateLimitDB, RateLimitKV),
		"rate_limit.driver: %q is not one of %s", c.RateLimit.Driver, joinValues(RateLimitDB, RateLimitKV))
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

	check(isOneOf(c.Session.Driver, SessionDB, SessionKV),
		"session.driver: %q is not one of %s", c.Session.Driver, joinValues(SessionDB, SessionKV))
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

// unsetNote describes a key left at its default because the variable its
// directive named is not set.
//
// An unset variable is reported only where it leaves the key unusable, not for
// every key that references one: app.mode falls back to development and
// auth.private_key to the HMAC secret, so naming those would report a choice the
// user made on purpose. The caller supplies the wording for the resolved case,
// so the message reads the same whether the file named a variable or not.
func (c Config) unsetNote(key, resolved string) string {
	name, ok := c.unresolved[key]
	if !ok {
		return resolved
	}
	return fmt.Sprintf("must not be empty: the %s variable is not set", name)
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
	return slices.Contains(accepted, value)
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

// kvStoreDrivers returns the feature keys whose driver is the key-value backend.
// It is what turns "a driver points at a switched-off backend" into a message
// that names the key, rather than a failure at start-up.
func (c Config) kvStoreDrivers() []string {
	var keys []string
	if c.Cache.Driver == CacheKV {
		keys = append(keys, "cache.driver")
	}
	if c.RateLimit.Driver == RateLimitKV {
		keys = append(keys, "rate_limit.driver")
	}
	if c.Session.Driver == SessionKV {
		keys = append(keys, "session.driver")
	}
	return keys
}

// isKVURL reports whether value is a key-value connection string.
//
// The rules mirror the URL grammar the Valkey and Redis clients accept
// (redis.ParseURL in github.com/redis/go-redis/v9), because validation that is
// stricter than the client rejects a URL that would have connected:
//
//   - redis, rediss (TLS), and unix (a unix socket) are the accepted schemes
//   - a missing host is not an error: the client defaults it to localhost:6379
//   - the path is the database index, and is empty or one integer segment
//   - a unix socket carries its path instead of a host
//
// A query parameter is left to the client, which rejects an unexpected one when
// it connects. Mirroring that list here would be a second copy of it to keep in
// step, and it does not decide whether the URL names a server.
func isKVURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "redis", "rediss":
		return isKVDBPath(parsed.Path)
	case "unix":
		return parsed.Path != ""
	default:
		return false
	}
}

// isKVDBPath reports whether a key-value URL path is the database index: empty,
// or a single integer segment. The client rejects anything else.
func isKVDBPath(path string) bool {
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	switch len(segments) {
	case 0:
		return true
	case 1:
		_, err := strconv.Atoi(segments[0])
		return err == nil
	default:
		return false
	}
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

// isEmail reports whether value is an email address. net/mail accepts a bare
// local part and a display name, so the address form is what is checked here:
// the config holds the address alone, and a display name belongs in FromName.
func isEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	if err != nil {
		return false
	}
	return address.Address == value && strings.Contains(value, "@")
}

// A caller validates a Config it did not write, and renders one safely when it
// prints it. Both act on a resolved Config, so both live here.

// redacted is the placeholder a Redacted Config prints instead of a secret.
const redacted = "[redacted]"

// Masking shows a little of a secret so a value can be traced back to its
// source. It is for a report the operator runs against their own configuration,
// never for a log line: see Masked.
const (
	// maskKeep is how many characters are kept at each end of a masked value.
	maskKeep = 4
	// maskSeparator stands in for the hidden middle. It is what makes a masked
	// value recognisable as masked rather than as a short secret.
	maskSeparator = "****"
	// maskMinLength is the shortest value that is masked at all. A shorter one
	// is redacted in full: keeping eight characters of a twelve-character
	// secret would leave most of it readable. Every secret this configuration
	// actually holds is far longer, so the floor only catches a short password,
	// where hiding it completely is the safe answer.
	maskMinLength = 16
)

// mask renders a secret for a report: the first and last few characters with
// the middle replaced, so two keys can be told apart by eye.
//
// An empty value stays empty, so an unset secret is told apart from a set one;
// a value below maskMinLength is redacted in full.
func mask(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) < maskMinLength {
		return redacted
	}
	return secret[:maskKeep] + maskSeparator + secret[len(secret)-maskKeep:]
}

// secretRenderer renders one secret for display.
type secretRenderer func(string) string

// withSecrets returns a copy with every secret passed through render.
//
// A connection string is always reduced to host:port/database rather than
// rendered: it is a composite value, so revealing part of the string says
// nothing, while the host is the part a reader needs.
func (c Config) withSecrets(render secretRenderer) Config {
	out := c
	out.App.SecretKey = render(c.App.SecretKey)
	out.Auth.PrivateKey = render(c.Auth.PrivateKey)
	out.Auth.PublicKey = render(c.Auth.PublicKey)
	out.Auth.SecretKey = render(c.Auth.SecretKey)
	out.Database.URL = RedactDSN(c.Database.URL)
	out.KVStore.URL = RedactKVURL(c.KVStore.URL)
	out.Mailer.SMTPPassword = render(c.Mailer.SMTPPassword)
	out.origin = nil
	out.unresolved = nil
	return out
}

// Redacted returns a copy with every secret replaced by a placeholder, safe to
// print or log.
//
// This is the fail-safe. A Config rendered by %v, or a report that has no
// business showing a secret at all, gets this one: it shows nothing of the
// value, so there is no partial leak to reason about.
func (c Config) Redacted() Config {
	return c.withSecrets(func(string) string { return redacted })
}

// Masked returns a copy with every secret partially hidden: enough of each value
// to recognise it, never enough to use it. It is what `config:print` renders.
//
// It is deliberately not what a log line gets. A log is read by everyone with
// access to the logs, so a partial secret there is a secret that leaked slowly;
// a report is read by the operator, who could read the value anyway. Keeping the
// two apart is what lets the fail-safe stay absolute.
func (c Config) Masked() Config {
	return c.withSecrets(mask)
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

// RedactKVURL reduces a key-value connection string to host:port/database, the
// same rendering RedactDSN gives a Postgres one.
//
// It does not reuse RedactDSN: that parse is pgx's, and a redis:// URL is not a
// Postgres connection string, so pgx rejects it and the whole value would be
// replaced by the placeholder. The point of the rendering is to name the server
// a command talks to, which a placeholder cannot do.
//
// A missing host is not a failure here either: the client would default it to
// localhost:6379, so the target says the same rather than hiding the URL.
func RedactKVURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return redacted
	}
	if parsed.Scheme == "unix" {
		return parsed.Path
	}
	target := parsed.Host
	if target == "" {
		target = "localhost:6379"
	}
	if db := strings.TrimPrefix(parsed.Path, "/"); db != "" {
		target += "/" + db
	}
	return target
}

// String renders the configuration with every secret redacted, so an accidental
// %v of a Config cannot leak a credential.
func (c Config) String() string {
	redacted := c.Redacted()
	return fmt.Sprintf("app=%s database=%s server=%s:%d log=%s/%s",
		redacted.App.Mode, RedactDSN(redacted.Database.URL), redacted.Server.Host,
		redacted.Server.Port, redacted.Log.Level, redacted.Log.Format)
}
