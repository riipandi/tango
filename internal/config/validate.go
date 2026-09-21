package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

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
	check(len(c.Log.Transport) > 0, "log.transport: at least one transport is required")
	// Every name is checked, not just the first: a list with one good name and
	// one typo would otherwise start with a sink silently missing, which is the
	// failure the list exists to prevent.
	for _, transport := range c.Log.Transport {
		check(isOneOf(transport, LogTransports()...),
			"log.transport: %q is not one of %s", transport, joinValues(LogTransports()...))
	}
	check(noDuplicates(c.Log.Transport),
		"log.transport: %s must not repeat a transport", strings.Join(c.Log.Transport, ", "))

	// The file sink is built only when it is named, so the rotation settings are
	// read only then: holding them to anything would report a problem in a part
	// of the file that is switched off.
	if c.logTransport(LogTransportFile) {
		check(c.Log.File.MaxSize > 0, "log.file.max_size: must be positive")
		check(c.Log.File.MaxBackups >= 0, "log.file.max_backups: must not be negative")
		check(c.Log.File.MaxAge >= 0, "log.file.max_age: must not be negative")
		// Zero on both is the one combination that never deletes a rotated
		// file, so it is refused here rather than filling a disk later.
		check(c.Log.File.MaxBackups > 0 || c.Log.File.MaxAge > 0,
			"log.file: set max_backups or max_age; zero on both keeps every rotated file")
	}

	// The collector is dialled only when it is named, so its endpoint is held to
	// a URL only then. The address is otel.endpoint for all three signals, so a
	// log-only deployment still reads it here.
	if c.logTransport(LogTransportOTLP) {
		check(c.OTEL.Endpoint != "", "otel.endpoint: %s",
			c.unsetNote("otel.endpoint", "must not be empty when log.transport names otlp"))
	}

	check(isOneOf(c.OTEL.Protocol, OTELProtocols()...),
		"otel.protocol: %q is not one of %s", c.OTEL.Protocol, joinValues(OTELProtocols()...))
	check(isOneOf(c.OTEL.Compression, OTELCompressions()...),
		"otel.compression: %q is not one of %s", c.OTEL.Compression, joinValues(OTELCompressions()...))
	check(c.OTEL.Queue.MaxSize > 0, "otel.queue.max_size: must be positive")

	// A header name becomes part of the request, so an empty one or a value
	// that cannot be a header value is refused here rather than at the first
	// export, where the exporter reports it far from the key that caused it.
	for name := range c.OTEL.Headers {
		check(name != "", "otel.headers: a header name must not be empty")
	}

	// http/json is the one protocol combination the Go exporters do not all
	// implement: only the trace exporter encodes JSON. Metrics and logs would
	// silently send protobuf to a collector expecting JSON, so the mismatch is
	// refused by name instead of shipped. The signals are named in one message
	// so a deployment that enabled both fixes both in one pass.
	if c.OTEL.Protocol == OTELProtocolHTTPJSON {
		check(len(c.jsonUnsupportedSignals()) == 0,
			"otel.protocol: %q is not supported for %s; use %q",
			OTELProtocolHTTPJSON, joinValues(c.jsonUnsupportedSignals()...), OTELProtocolHTTPProtobuf)
	}

	// The address is checked whenever any signal is enabled, and the scheme
	// decides TLS, so a bad one fails here rather than at the first export. gRPC
	// addresses a service by host and port, so it is held to that instead of to
	// a URL.
	if c.otelEnabled() {
		check(c.OTEL.Endpoint != "", "otel.endpoint: %s",
			c.unsetNote("otel.endpoint", "must not be empty when a signal is enabled"))
		if UsesHTTP(c.OTEL.Protocol) {
			check(c.OTEL.Endpoint == "" || isHTTPURL(c.OTEL.Endpoint),
				"otel.endpoint: %q must be an absolute http or https URL for protocol %q",
				c.OTEL.Endpoint, c.OTEL.Protocol)
		} else {
			check(c.OTEL.Endpoint == "" || isGRPCTarget(c.OTEL.Endpoint),
				"otel.endpoint: %q must be host:port for protocol %q", c.OTEL.Endpoint, c.OTEL.Protocol)
			// The HTTP default port is the trap this catches: switching the
			// protocol without moving the address is the one change that looks
			// applied and sends nothing, because the collector's two protocols
			// are two listeners. A host other than the default's is left alone,
			// since a deployment may legitimately front both on one port.
			check(!strings.HasSuffix(c.OTEL.Endpoint, ":"+DefaultOTELHTTPPort),
				"otel.endpoint: %q is the HTTP port; protocol %q listens on %s",
				c.OTEL.Endpoint, c.OTEL.Protocol, DefaultOTELGRPCPort)
		}
		check(c.OTEL.ServiceName != "", "otel.service_name: must not be empty")
	}

	// The trace section is read only when tracing is switched on: holding a
	// sampler that never runs to anything would report a problem in a part of
	// the file nothing reads.
	if c.OTEL.Tracing.Enable {
		check(isOneOf(c.OTEL.Tracing.Sampler, OTELSamplers()...),
			"otel.tracing.sampler: %q is not one of %s",
			c.OTEL.Tracing.Sampler, joinValues(OTELSamplers()...))
		// A ratio is read by two samplers and ignored by the other two, so a
		// value outside 0..1 is refused here rather than silently doing nothing.
		if usesOTELRatio(c.OTEL.Tracing.Sampler) {
			check(c.OTEL.Tracing.Ratio >= 0 && c.OTEL.Tracing.Ratio <= 1,
				"otel.tracing.ratio: %v must be between 0 and 1 for sampler %q",
				c.OTEL.Tracing.Ratio, c.OTEL.Tracing.Sampler)
		}
		check(c.OTEL.Tracing.BatchTimeout > 0, "otel.tracing.batch_timeout: must be positive")
		check(c.OTEL.Tracing.ExportTimeout > 0, "otel.tracing.export_timeout: must be positive")
		check(c.OTEL.Tracing.MaxBatchSize > 0, "otel.tracing.max_batch_size: must be positive")
	}

	if c.OTEL.Metrics.Enable {
		check(isPath(c.OTEL.Metrics.PrometheusPath),
			"otel.metrics.prometheus_path: %q must be a path such as /metrics", c.OTEL.Metrics.PrometheusPath)
		check(c.OTEL.Metrics.Interval > 0, "otel.metrics.interval: must be positive")
		check(c.OTEL.Metrics.ExportTimeout > 0, "otel.metrics.export_timeout: must be positive")
	}

	// A route is read only when the signal that owns it ships to a collector:
	// the path of a signal nothing exports would be a value nothing reads.
	if c.logTransport(LogTransportOTLP) {
		check(isOTELPath(c.Log.OTLP.Path),
			"log.otlp.path: %q must be a path such as /v1/logs", c.Log.OTLP.Path)
	}
	if c.OTEL.Tracing.Enable {
		check(isOTELPath(c.OTEL.Tracing.Path),
			"otel.tracing.path: %q must be a path such as /v1/traces", c.OTEL.Tracing.Path)
	}
	if c.OTEL.Metrics.Enable {
		check(isOTELPath(c.OTEL.Metrics.Path),
			"otel.metrics.path: %q must be a path such as /v1/metrics", c.OTEL.Metrics.Path)
	}

	// A gRPC exporter is addressed by host and port alone, so a per-signal path
	// is a setting the exporter never reads. The combination is refused by name
	// rather than silently ignored: a path that says where a signal goes while
	// nothing follows it is the trap a configuration check exists for.
	if c.OTEL.Protocol == OTELProtocolGRPC {
		paths := []struct{ key, value string }{}
		if c.logTransport(LogTransportOTLP) {
			paths = append(paths, struct{ key, value string }{"log.otlp.path", c.Log.OTLP.Path})
		}
		if c.OTEL.Tracing.Enable {
			paths = append(paths, struct{ key, value string }{"otel.tracing.path", c.OTEL.Tracing.Path})
		}
		if c.OTEL.Metrics.Enable {
			paths = append(paths, struct{ key, value string }{"otel.metrics.path", c.OTEL.Metrics.Path})
		}
		for _, p := range paths {
			check(p.value == "",
				"%s: %q has no effect with protocol %q; a gRPC exporter is addressed by host and port",
				p.key, p.value, OTELProtocolGRPC)
		}
	}

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

	// The S3 section is checked only when the driver selects it. A local
	// deployment never dials an object store, so holding its settings to
	// anything would report a problem in a part of the file that is switched
	// off, and a user keeping credentials there for a later switch could not
	// run at all.
	if c.Storage.Driver == StorageS3 {
		s3 := c.Storage.S3
		check(s3.BucketName != "", "storage.s3.bucket_name: %s",
			c.unsetNote("storage.s3.bucket_name", "must not be empty when storage.driver is s3"))
		check(s3.Region != "", "storage.s3.region: %s",
			c.unsetNote("storage.s3.region", "must not be empty when storage.driver is s3"))
		check(s3.AccessKeyID != "", "storage.s3.access_key_id: %s",
			c.unsetNote("storage.s3.access_key_id", "must not be empty when storage.driver is s3"))
		check(s3.AccessKeySecret != "", "storage.s3.access_key_secret: %s",
			c.unsetNote("storage.s3.access_key_secret", "must not be empty when storage.driver is s3"))
		check(s3.EndpointURL == "" || isHTTPURL(s3.EndpointURL),
			"storage.s3.endpoint_url: %q must be an absolute http or https URL", s3.EndpointURL)
		// The protocol caps a signed link at seven days, and the client takes a
		// zero as "use my own fifteen-minute default" rather than as a
		// lifetime, so both ends are held here instead of failing at signing
		// time.
		check(s3.SignedURLExpires > 0 && s3.SignedURLExpires <= maxS3SignedURLExpires,
			"storage.s3.signed_url_expires: %s must be between 1s and %s",
			s3.SignedURLExpires, maxS3SignedURLExpires)
	}

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

// noDuplicates reports whether every value appears once. A repeated value is a
// mistake worth naming: the list is read as "which sinks", and naming one twice
// says nothing a reader can act on.
func noDuplicates(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
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

// logTransport reports whether the configured transport list names one sink.
// It is how a section is read only when the sink that owns it is switched on.
func (c Config) logTransport(name string) bool {
	return slices.Contains(c.Log.Transport, name)
}

// otelEnabled reports whether any signal ships to the collector. It is what
// makes the shared endpoint and service name required: with every signal off,
// nothing dials and the values are not read.
func (c Config) otelEnabled() bool {
	return c.logTransport(LogTransportOTLP) || c.OTEL.Tracing.Enable || c.OTEL.Metrics.Enable
}

// jsonUnsupportedSignals names the enabled signals whose exporter cannot encode
// JSON. It is what turns the one protocol the Go SDK implements unevenly into a
// message naming the signal that would be wrong, rather than a bare refusal of a
// value the specification allows.
func (c Config) jsonUnsupportedSignals() []string {
	var signals []string
	if c.OTEL.Metrics.Enable {
		signals = append(signals, "metrics")
	}
	if c.logTransport(LogTransportOTLP) {
		signals = append(signals, "logs")
	}
	return signals
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

// isGRPCTarget reports whether value is what a gRPC exporter accepts as an
// address: a bare host:port, such as localhost:4317.
//
// A scheme is refused rather than stripped. The gRPC exporter takes the host
// from a URL and ignores everything else, so http://localhost:4317 would work
// while looking like it meant something — and a user who writes it has probably
// mistaken the port as well. Naming the expected form is more useful than
// quietly accepting one that is nearly right.
func isGRPCTarget(value string) bool {
	if strings.Contains(value, "://") || strings.Contains(value, "/") {
		return false
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}
	return port != ""
}

// isOTELPath reports whether value is a usable OTLP route: empty, or a URL path
// that starts with a slash and carries no scheme or host.
//
// It is what a signal's collector route is held to. A full URL there would be a
// second way to name a collector, which is the thing one shared otel.endpoint
// exists to prevent, so it is refused rather than merged. Empty is accepted
// because it means the protocol's own route.
func isOTELPath(value string) bool {
	if value == "" {
		return true
	}
	if !strings.HasPrefix(value, "/") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return parsed.Scheme == "" && parsed.Host == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

// isPath reports whether value is a URL path: it starts with a slash and
// carries no scheme or host. It is what the Prometheus endpoint is held to,
// which must name a path rather than be empty.
func isPath(value string) bool {
	return value != "" && isOTELPath(value)
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

// maxS3SignedURLExpires is the longest lifetime a presigned URL may carry. The
// S3 protocol caps a signed link at seven days; a client that asked for longer
// would be refused by the server at signing time, so the limit is enforced where
// the value is read.
const maxS3SignedURLExpires = 7 * 24 * time.Hour

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

// redactHeaders renders every header value through the secret path.
//
// A header is a secret as a whole rather than by name: an authorization token is
// why the key exists, and a map is one config value, so there is no key to list
// per entry. The names are kept, because a name says which credential is missing
// without revealing it, and they are read by a person rather than a matcher.
func redactHeaders(headers map[string]string, render secretRenderer) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for name, value := range headers {
		out[name] = render(value)
	}
	return out
}

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
	out.Storage.S3.AccessKeyID = render(c.Storage.S3.AccessKeyID)
	out.Storage.S3.AccessKeySecret = render(c.Storage.S3.AccessKeySecret)
	out.OTEL.Headers = redactHeaders(c.OTEL.Headers, render)
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
