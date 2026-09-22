package config

import (
	"net/url"
	"time"
)

// Config is the resolved application configuration. It is the result of merging
// the built-in defaults, the JSON config file, and the command-line flags, in
// that order. The environment is not a layer: it is the table the config file's
// directives resolve from.
//
// The struct is the schema: every key has a koanf tag (how a source names it),
// a json tag (how it is written back out, and how Default is flattened), a
// default in Default(), and a rule in Validate().
type Config struct {
	App       App       `koanf:"app" json:"app"`
	Auth      Auth      `koanf:"auth" json:"auth"`
	Cache     Cache     `koanf:"cache" json:"cache"`
	Database  Database  `koanf:"database" json:"database"`
	KVStore   KVStore   `koanf:"kvstore" json:"kvstore"`
	Log       Log       `koanf:"log" json:"log"`
	Mailer    Mailer    `koanf:"mailer" json:"mailer"`
	OTEL      OTEL      `koanf:"otel" json:"otel"`
	Queue     Queue     `koanf:"queue" json:"queue"`
	RateLimit RateLimit `koanf:"rate_limit" json:"rate_limit"`
	Scheduler Scheduler `koanf:"scheduler" json:"scheduler"`
	Server    Server    `koanf:"server" json:"server"`
	Session   Session   `koanf:"session" json:"session"`
	Storage   Storage   `koanf:"storage" json:"storage"`

	// origin records which source last set each key, for conflict resolution.
	// It is unexported so it never reaches JSON or a log line.
	origin map[string]string
	// unresolved records the keys whose directive named a missing variable,
	// mapped to the variable name. It is state about the sources, not config.
	unresolved map[string]string
}

// App holds process-level settings.
type App struct {
	// Mode names the runtime mode: development, staging, production, or test.
	Mode string `koanf:"mode" json:"mode"`
	// SecretKey is the hex-encoded AES-256 key used to seal stored values.
	SecretKey string `koanf:"secret_key" json:"secret_key"`
	// BaseURL is the public origin, used to build absolute links.
	BaseURL string `koanf:"base_url" json:"base_url"`
	// AssetsURL is where the browser fetches static assets from: the built-in
	// /static mount today, an S3 bucket or a CDN origin whenever a deployment
	// points the variable there.
	AssetsURL string `koanf:"assets_url" json:"assets_url"`
}

// Auth holds the JWT signing material.
type Auth struct {
	// PrivateKey and PublicKey are base64 (raw, unpadded) JWK JSON.
	PrivateKey string `koanf:"private_key" json:"private_key"`
	PublicKey  string `koanf:"public_key" json:"public_key"`
	// SecretKey is the hex-encoded HMAC key, used when no key pair is given.
	SecretKey string `koanf:"secret_key" json:"secret_key"`
	// Issuer is the iss claim placed in every token.
	Issuer string `koanf:"issuer" json:"issuer"`
	// AccessTTL is the lifetime of an access token.
	AccessTTL time.Duration `koanf:"access_ttl" json:"access_ttl"`
	// RefreshTTL is the lifetime of a refresh token.
	RefreshTTL time.Duration `koanf:"refresh_ttl" json:"refresh_ttl"`
}

// Cache holds the key-value cache settings.
type Cache struct {
	// Enable switches the cache on. Disabled by default, so a feature that
	// has not decided to be cacheable cannot grow one by accident; a driver
	// that is configured while the cache is off is never read.
	Enable bool `koanf:"enable" json:"enable"`
	// Driver is CacheMemory or CacheKV. It is read only while Enable is
	// true, the way a signal section is read only while the signal is on.
	Driver string `koanf:"driver" json:"driver"`
	// TTL is the default lifetime of a cached entry.
	TTL time.Duration `koanf:"ttl" json:"ttl"`
	// MaxMemory is the byte budget of the in-memory driver. When the budget
	// runs out the driver resets itself, keeping the memory it already owns
	// rather than growing without bound.
	MaxMemory int64 `koanf:"max_memory" json:"max_memory"`
}

// Database holds the Postgres connection and pool settings. The fields mirror
// datastore.PostgresOptions, so a resolved Config maps onto a pool directly.
type Database struct {
	// URL is the connection string. It is the one secret that must never be
	// logged; use Redacted before printing a Config.
	URL string `koanf:"url" json:"url"`
	// MaxConns and MinConns bound the pool size.
	MaxConns int32 `koanf:"max_conns" json:"max_conns"`
	MinConns int32 `koanf:"min_conns" json:"min_conns"`
	// MaxConnLifetime and MaxConnIdleTime recycle pooled connections.
	MaxConnLifetime time.Duration `koanf:"max_conn_lifetime" json:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `koanf:"max_conn_idle_time" json:"max_conn_idle_time"`
	// ConnectTimeout bounds the initial connection attempt.
	ConnectTimeout time.Duration `koanf:"connect_timeout" json:"connect_timeout"`
	// SearchPath and Timezone are applied to every pooled connection.
	SearchPath string `koanf:"search_path" json:"search_path"`
	Timezone   string `koanf:"timezone" json:"timezone"`
}

// KVStore holds the optional key-value backend settings, for a Valkey or Redis
// compatible server.
//
// It is opt-in and never required: with Enable false the application runs on
// Postgres and in-process memory alone, which is what keeps a local checkout
// from needing a second server. Enable gates availability; the driver fields on
// Cache, Session, and RateLimit still choose which backend each one uses, so
// switching the kvstore on does not silently move anything.
type KVStore struct {
	// Enable reports whether the key-value backend is available. A driver set to
	// kvstore while this is false is a contradiction Validate reports.
	Enable bool `koanf:"enable" json:"enable"`
	// URL is the connection string, such as
	// redis://default:password@localhost:6379. It carries a password, so Redacted
	// hides it and a report reduces it to host:port/database.
	URL string `koanf:"url" json:"url"`
	// DB is the logical database index to select.
	DB int `koanf:"db" json:"db"`
}

// Log holds the logging settings.
type Log struct {
	// Level is one of debug, info, warn, or error.
	Level string `koanf:"level" json:"level"`
	// Transport names the sinks to write to, in the order given. More than one
	// may be named, so a run can keep the terminal and ship to a collector at
	// once. The default is the console alone, which is what a fresh checkout
	// needs and what a container that logs to stdout wants.
	//
	// It is a list rather than a set of switches because the sinks are not
	// alternatives: naming one is what turns it on, and there is no second flag
	// that could disagree with the list.
	Transport []string `koanf:"transport" json:"transport"`
	// Console holds the console sink settings, read when Transport names
	// LogTransportConsole.
	Console LogConsole `koanf:"console" json:"console"`
	// File holds the rotating file sink settings, read when Transport names
	// LogTransportFile.
	File LogFile `koanf:"file" json:"file"`
	// OTLP holds the log export settings that are specific to logs, read when
	// Transport names LogTransportOTLP.
	OTLP LogOTLP `koanf:"otlp" json:"otlp"`
}

// LogConsole holds the console sink settings.
//
// The console is the only sink with a rendering choice, because it is the one
// a person reads: a file or a collector keeps its machine form regardless, so
// no other sink has a key to choose it.
type LogConsole struct {
	// Format is LogPretty or LogStructured.
	Format string `koanf:"format" json:"format"`
}

// LogOTLP holds the log export settings that are specific to logs.
//
// The address is deliberately not here: one collector receives every signal, so
// where it is belongs to otel.endpoint, and a second copy of it could disagree.
// What logs own is the route they take on that collector, which is where a
// collector configured to split a signal puts them. It sits under log rather
// than under otel because it is a property of this sink, the way log.file holds
// the rotation settings of the file sink.
type LogOTLP struct {
	// Path is the collector route for logs. Empty means the protocol's own
	// /v1/logs, which is what a collector serves.
	Path string `koanf:"path" json:"path"`
	// Timeout bounds one export attempt, the way otel.tracing.export_timeout
	// bounds a trace export. Written as a plain number of seconds.
	Timeout time.Duration `koanf:"timeout" json:"timeout"`
}

// LogFile holds the rotating file sink settings.
//
// There is no filename key: the sink writes under the one data directory of the
// process, storage.local_path + /logs, so a run cannot disagree with the
// configuration about where its files live. The rotation settings are read only
// when the file transport is named.
type LogFile struct {
	// MaxSize is the size in megabytes the active file reaches before it is
	// rotated.
	MaxSize int `koanf:"max_size" json:"max_size"`
	// MaxBackups is how many rotated files are kept, and MaxAge how many days
	// one is kept for. They are independent: a file is deleted when either
	// limit is exceeded, so zero on both keeps every rotated file forever.
	MaxBackups int `koanf:"max_backups" json:"max_backups"`
	MaxAge     int `koanf:"max_age" json:"max_age"`
	// Compress gzips a rotated file.
	Compress bool `koanf:"compress" json:"compress"`
}

// OTEL holds the OpenTelemetry settings the three signals share, plus the
// section each one owns.
//
// One collector address serves all three, because that is what a collector is:
// a single endpoint that receives logs, traces, and metrics. A signal whose
// collector routes it elsewhere overrides only its own path, never its own
// address, so there is one place that says where the collector is.
//
// Every signal is opt-in and none is required, like every other external
// backend: a run that enables nothing dials nothing and needs no collector.
type OTEL struct {
	// Endpoint is the collector's OTLP address: http://localhost:4318 or
	// https://collector.example.com:4318 for an HTTP protocol, and
	// localhost:4317 for gRPC. The scheme decides whether an HTTP connection is
	// TLS, so it is not a separate setting.
	//
	// One address serves every signal, including the protocol that addresses a
	// service by host and port alone: an HTTP collector and a gRPC collector are
	// two listeners of one collector, so a deployment that runs both points this
	// at the one its signals use.
	//
	// It is read when any signal is enabled: logs name the otlp transport,
	// traces set tracing.enable, or metrics set metrics.enable.
	Endpoint string `koanf:"endpoint" json:"endpoint"`
	// ServiceName is the service every signal is attributed to. It defaults to
	// the application identifier rather than to an empty string, because a
	// record with no service name cannot be attributed at all.
	ServiceName string `koanf:"service_name" json:"service_name"`
	// Environment names the deployment a signal came from, such as production
	// or staging. It is a resource attribute, so one collector can tell two
	// deployments apart.
	Environment string `koanf:"environment" json:"environment"`
	// Protocol is one of OTELProtocols, the wire protocol every signal is sent
	// with. It is shared rather than per-signal because a collector accepts one
	// protocol per listener: a deployment that needs two would be running two
	// collectors, which is what a second address would be.
	Protocol string `koanf:"protocol" json:"protocol"`
	// Compression is OTELCompressionGzip or OTELCompressionNone. Gzip is the
	// protocol's own default; none saves the CPU on a collector reached over a
	// loopback or a local network.
	Compression string `koanf:"compression" json:"compression"`
	// Headers are sent with every export, for a collector that authenticates
	// the sender. They are shared by the three signals because the one collector
	// they reach is the one that checks them.
	//
	// A header value is a secret by default: an authorization token is the
	// reason this key exists at all, and a value that must not be logged cannot
	// be told from one that may, so every value is rendered through the same
	// path a secret takes.
	Headers map[string]string `koanf:"headers" json:"headers"`
	// Queue bounds the in-memory buffer each signal exports from.
	Queue OTELQueue `koanf:"queue" json:"queue"`
	// Tracing holds the trace export settings.
	Tracing OTELTracing `koanf:"tracing" json:"tracing"`
	// Metrics holds the metric export settings.
	Metrics OTELMetrics `koanf:"metrics" json:"metrics"`
}

// OTELQueue bounds the buffer a signal exports from.
//
// It is the setting that keeps export off the request path: a span or a
// measurement is handed to an in-memory queue and the caller returns, while a
// background goroutine drains the queue to the collector. Nothing here ever
// blocks the goroutine that produced a signal, so a slow or unreachable
// collector costs dropped telemetry, never a slow request.
type OTELQueue struct {
	// MaxSize is how many items one signal buffers before it starts dropping.
	// The default is generous rather than minimal, because the queue is what
	// absorbs a collector that is briefly down.
	MaxSize int `koanf:"max_size" json:"max_size"`
}

// OTELTracing holds the trace export settings.
//
// It is read only when Enable is true, so a deployment that does not collect
// traces is not held to a sampler it never runs.
type OTELTracing struct {
	// Enable exports spans. A service that does not trace dials nothing.
	Enable bool `koanf:"enable" json:"enable"`
	// Path is the collector route for traces. Empty means the protocol's own
	// /v1/traces, which is what a collector serves.
	Path string `koanf:"path" json:"path"`
	// Sampler is one of OTELSamplers. It decides which traces are recorded.
	Sampler string `koanf:"sampler" json:"sampler"`
	// Ratio is the fraction of traces recorded, and is read only by the two
	// ratio samplers. It is a fraction rather than a percentage so the value
	// reads the way the sampler's own name does.
	Ratio float64 `koanf:"ratio" json:"ratio"`
	// BatchTimeout is how long a span waits in the queue before the exporter
	// ships it, and ExportTimeout bounds one export attempt.
	BatchTimeout  time.Duration `koanf:"batch_timeout" json:"batch_timeout"`
	ExportTimeout time.Duration `koanf:"export_timeout" json:"export_timeout"`
	// MaxBatchSize is how many spans one export carries.
	MaxBatchSize int `koanf:"max_batch_size" json:"max_batch_size"`
}

// OTELMetrics holds the metric export settings.
//
// It is read only when Enable is true. Metrics leave by two routes at once, and
// both are wanted: the push to the collector follows the other signals, and the
// /metrics endpoint is what a Prometheus-style scraper reads. A deployment that
// runs a scraper but no collector is served by the same switch.
type OTELMetrics struct {
	// Enable records and exports metrics.
	Enable bool `koanf:"enable" json:"enable"`
	// Path is the collector route for metrics. Empty means the protocol's own
	// /v1/metrics.
	Path string `koanf:"path" json:"path"`
	// PrometheusPath is where the Prometheus exposition is served, on the
	// application's own port. It is a path rather than a switch: the exposition
	// is always served when metrics are enabled, and a scrape job needs the
	// path to be a decision rather than a second enable flag.
	PrometheusPath string `koanf:"prometheus_path" json:"prometheus_path"`
	// Interval is how often measurements are handed to the exporter, and
	// ExportTimeout bounds one export attempt.
	Interval      time.Duration `koanf:"interval" json:"interval"`
	ExportTimeout time.Duration `koanf:"export_timeout" json:"export_timeout"`
}

// Mailer holds the outbound email settings. The mailer is optional: with no SMTP
// host the application still runs, it just cannot send mail. That is what keeps
// a local checkout from needing a mail server.
type Mailer struct {
	// FromEmail and FromName are the sender every message is sent as.
	FromEmail string `koanf:"from_email" json:"from_email"`
	FromName  string `koanf:"from_name" json:"from_name"`
	// SMTPHost is the mail server. An empty host means the mailer is not
	// configured, and it is not a configuration error.
	SMTPHost string `koanf:"smtp_host" json:"smtp_host"`
	// SMTPPort is the submission port: 587 for STARTTLS, 465 for implicit TLS.
	SMTPPort int `koanf:"smtp_port" json:"smtp_port"`
	// SMTPUsername and SMTPPassword authenticate the session. The password is a
	// secret, so Redacted hides it and Sample writes it as a directive.
	SMTPUsername string `koanf:"smtp_username" json:"smtp_username"`
	SMTPPassword string `koanf:"smtp_password" json:"smtp_password"`
	// SMTPSecure selects implicit TLS on connect instead of STARTTLS.
	SMTPSecure bool `koanf:"smtp_secure" json:"smtp_secure"`
}

// Queue holds the background task queue settings. The queue runs on the same
// Postgres the application already uses, so there is no backend to enable: a
// serve run carries a worker pool, and the values here size it.
type Queue struct {
	// NumWorkers is the number of goroutines that execute queued tasks
	// concurrently.
	NumWorkers int `koanf:"num_workers" json:"num_workers"`
	// ReleaseAfter is how long a claimed task may run before the queue
	// considers its worker lost and hands the task to another one. It must
	// exceed the longest execution a queue's Timeout allows, or a slow task
	// would be run twice.
	ReleaseAfter time.Duration `koanf:"release_after" json:"release_after"`
	// CleanupInterval is how often the maintenance job deletes the completed
	// records their retention has expired.
	CleanupInterval time.Duration `koanf:"cleanup_interval" json:"cleanup_interval"`
	// Encrypt seals task payloads at rest with the application secret
	// (app.secret_key). A task still in flight when the flag flips is read
	// as the plaintext it is; every task written afterwards is sealed.
	Encrypt bool `koanf:"encrypt" json:"encrypt"`
}

// Scheduler holds the cron scheduler settings. The scheduler enqueues tasks
// onto the queue at cron times and claims each tick in Postgres, so the
// schedule survives a restart and one replica only fires a tick.
type Scheduler struct {
	// Timezone resolves a spec that names no zone of its own, so "0 3 * * *"
	// is three in the morning somewhere in particular.
	Timezone string `koanf:"timezone" json:"timezone"`
}

// RateLimit holds the request throttling settings.
type RateLimit struct {
	// Driver is RateLimitDB or RateLimitKV.
	Driver string `koanf:"driver" json:"driver"`
	// Limit is the number of requests allowed per Window.
	Limit int `koanf:"limit" json:"limit"`
	// Window is the period the Limit applies to.
	Window time.Duration `koanf:"window" json:"window"`
}

// Server holds the HTTP server settings.
type Server struct {
	// Host and Port are the listen address.
	Host string `koanf:"host" json:"host"`
	Port int    `koanf:"port" json:"port"`
	// ReadTimeout, WriteTimeout, and IdleTimeout are the net/http timeouts.
	ReadTimeout  time.Duration `koanf:"read_timeout" json:"read_timeout"`
	WriteTimeout time.Duration `koanf:"write_timeout" json:"write_timeout"`
	IdleTimeout  time.Duration `koanf:"idle_timeout" json:"idle_timeout"`
	// ShutdownTimeout bounds the graceful shutdown drain.
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout" json:"shutdown_timeout"`
	// CORS holds the browser cross-origin policy for the API.
	CORS CORS `koanf:"cors" json:"cors"`
}

// CORS holds the browser cross-origin policy, applied to every route by the
// transport middleware. The lists are explicit rather than a single on/off
// switch, because an API that names its origins can also drop the wildcard.
//
// An empty AllowedOrigins disables cross-origin access, which is the honest
// default for a same-origin SPA; a deployment that serves the SPA from another
// origin lists it.
type CORS struct {
	// AllowedOrigins lists the origins a browser may call from. Each is a
	// scheme://host origin without a path, or "*" for any origin, which
	// AllowCredentials forbids combining with.
	AllowedOrigins []string `koanf:"allowed_origins" json:"allowed_origins"`
	// AllowedMethods lists the HTTP methods a cross-origin request may use.
	AllowedMethods []string `koanf:"allowed_methods" json:"allowed_methods"`
	// AllowedHeaders lists the request headers a cross-origin call may set.
	AllowedHeaders []string `koanf:"allowed_headers" json:"allowed_headers"`
	// AllowCredentials lets a cross-origin call carry cookies and credentials.
	// The CORS specification forbids credentials with a wildcard origin, so
	// Validate refuses that combination.
	AllowCredentials bool `koanf:"allow_credentials" json:"allow_credentials"`
	// MaxAge is how long a browser may cache a preflight answer.
	MaxAge time.Duration `koanf:"max_age" json:"max_age"`
}

// Session holds the session store settings.
type Session struct {
	// Driver is SessionDB or SessionKV.
	Driver string `koanf:"driver" json:"driver"`
	// TTL is how long an idle session stays valid.
	TTL time.Duration `koanf:"ttl" json:"ttl"`
}

// Storage holds the file storage settings.
type Storage struct {
	// Driver is StorageLocal or StorageS3.
	Driver string `koanf:"driver" json:"driver"`
	// LocalPath is the directory the local driver writes to, relative to the
	// working directory or absolute. It is the one data directory of the
	// process: the backup default and the storage health check both read it, so
	// there is no second path to disagree with it.
	LocalPath string `koanf:"local_path" json:"local_path"`
	// ChunkSize is the size of one chunk an uploaded file is split into, in
	// bytes. The chunk hashes are the manifest, so only a chunk that changed
	// is uploaded again.
	ChunkSize int `koanf:"chunk_size" json:"chunk_size"`
	// Watch holds the staging watcher: it notices a finished or changed
	// staging file and enqueues the chunk upload.
	Watch Watch `koanf:"watch" json:"watch"`
	// S3 holds the object-storage settings, used when Driver is StorageS3.
	S3 S3 `koanf:"s3" json:"s3"`
}

// Watch holds the staging directory watcher settings.
type Watch struct {
	// Enable switches the staging watcher on. Without it, staging files sit
	// until something enqueues their upload itself.
	Enable bool `koanf:"enable" json:"enable"`
	// Debounce is how long a staging path must stay quiet before its upload
	// is enqueued, so a file written in many small writes triggers one
	// upload, not one per write.
	Debounce time.Duration `koanf:"debounce" json:"debounce"`
}

// S3 holds the object-storage settings for an S3-compatible service, which may
// be AWS or anything speaking the same protocol.
//
// The section is nested rather than flattened because its keys cover several
// concerns at once — credentials, the bucket, the endpoint, the addressing
// style, and the lifetime of a signed link. A common prefix on every one of them
// would be noise the nesting already carries.
type S3 struct {
	// AccessKeyID and AccessKeySecret authenticate every request. Both are
	// secrets, so Redacted hides them and Sample writes them as directives.
	AccessKeyID     string `koanf:"access_key_id" json:"access_key_id"`
	AccessKeySecret string `koanf:"access_key_secret" json:"access_key_secret"`
	// BucketName is the bucket objects are written to.
	BucketName string `koanf:"bucket_name" json:"bucket_name"`
	// EndpointURL is the base URL of a service other than AWS, such as
	// http://localhost:9100. Empty means AWS, addressed through Region.
	EndpointURL string `koanf:"endpoint_url" json:"endpoint_url"`
	// ForcePathStyle addresses a bucket as a path segment (host/bucket/key)
	// instead of a subdomain (bucket.host/key). MinIO and Silo need it: the
	// client does not fall back on its own, and bucket.localhost does not
	// resolve.
	ForcePathStyle bool `koanf:"force_path_style" json:"force_path_style"`
	// PathPrefix is the key prefix objects are stored under. Empty means the
	// bucket root.
	PathPrefix string `koanf:"path_prefix" json:"path_prefix"`
	// Region is the signing region. It is required even when EndpointURL is
	// set, because the client refuses to resolve an endpoint without one, so
	// the default is a usable value rather than an empty string.
	Region string `koanf:"region" json:"region"`
	// SignedURLExpires is how long a presigned link stays valid.
	SignedURLExpires time.Duration `koanf:"signed_url_expires" json:"signed_url_expires"`
}

// Supported values for the driver and format fields. A driver is opt-in: the
// default is always the dependency-free choice, so a fresh checkout runs on
// Postgres and the local filesystem alone.
//
// A key-value backend is named "kvstore" rather than after the product behind
// it, so the configuration does not have to change if that choice does.
const (
	CacheMemory   = "memory"
	CacheKV       = "kvstore"
	LogPretty     = "pretty"
	LogStructured = "structured"
	RateLimitDB   = "database"
	RateLimitKV   = "kvstore"
	SessionDB     = "database"
	SessionKV     = "kvstore"
	StorageLocal  = "local"
	StorageS3     = "s3"
)

// Sampler names OTEL.Tracing.Sampler accepts, mirroring the OpenTelemetry
// samplers: always records every trace, never records any, and the two ratio
// forms record a fraction of them.
const (
	OTELSamplerAlways = "always"
	OTELSamplerNever  = "never"
	OTELSamplerRatio  = "ratio"
	// OTELSamplerParentRatio records a fraction of the traces that start fresh
	// and follows the decision of a parent for the rest, so a service that
	// receives a sampled request keeps the trace whole.
	OTELSamplerParentRatio = "parent_ratio"
)

// OTELSamplers returns every accepted sampler name, in the order the
// documentation lists them.
func OTELSamplers() []string {
	return []string{OTELSamplerAlways, OTELSamplerNever, OTELSamplerRatio, OTELSamplerParentRatio}
}

// OTEL.Tracing.Sampler accepts these two ratios, which are the ones that read
// Tracing.Ratio. Every other sampler ignores it, so Validate refuses a ratio
// that nothing would read rather than leaving a user with a value that looks
// applied and is not.
func usesOTELRatio(sampler string) bool {
	return sampler == OTELSamplerRatio || sampler == OTELSamplerParentRatio
}

// Protocol names OTEL.Protocol accepts, spelled the way the OpenTelemetry
// specification spells them so a value can be copied from its documentation.
const (
	// OTELProtocolGRPC is the protobuf payload over gRPC, on the protocol's own
	// port (4317).
	OTELProtocolGRPC = "grpc"
	// OTELProtocolHTTPProtobuf is the protobuf payload over HTTP, on port 4318.
	// It is the default here because it is the protocol an HTTP deployment can
	// put behind the same proxy, TLS terminator, and firewall rule as the rest
	// of its traffic.
	OTELProtocolHTTPProtobuf = "http/protobuf"
	// OTELProtocolHTTPJSON is the JSON payload over HTTP. It is readable with
	// curl, which is what makes it useful against a collector being debugged.
	//
	// The Go exporters implement it unevenly — only the trace exporter encodes
	// JSON — so Validate refuses it for metrics and logs rather than sending
	// them as protobuf to a collector expecting JSON.
	OTELProtocolHTTPJSON = "http/json"
)

// OTELProtocols returns every accepted protocol name.
//
// The three are the whole set the specification defines. A name outside it is
// refused rather than mapped to a default, because a deployment that asked for
// one protocol and silently got another has telemetry its collector may reject
// without saying so.
func OTELProtocols() []string {
	return []string{OTELProtocolGRPC, OTELProtocolHTTPProtobuf, OTELProtocolHTTPJSON}
}

// UsesHTTP reports whether the protocol travels over HTTP, which is what decides
// whether a signal's path and the endpoint's own path mean anything. A gRPC
// service is addressed by host and port alone.
func UsesHTTP(protocol string) bool {
	return protocol != OTELProtocolGRPC
}

// CollectorEndpoint returns the collector address in the form the configured
// protocol's exporter wants.
//
// The two forms differ because the two protocols address a service differently.
// An HTTP exporter wants the URL it can dial, scheme included. A gRPC exporter
// wants host:port and takes the scheme from the credentials instead, so an
// endpoint written as a URL is reduced to its host and port: the same value
// serves both protocols, which is what one shared endpoint means.
func (c Config) CollectorEndpoint() string {
	if UsesHTTP(c.OTEL.Protocol) {
		return c.OTEL.Endpoint
	}
	parsed, err := url.Parse(c.OTEL.Endpoint)
	if err != nil || parsed.Host == "" {
		return c.OTEL.Endpoint
	}
	return parsed.Host
}

// CollectorSecure reports whether the collector connection uses TLS.
//
// It is read from the endpoint's own scheme, so there is no second setting that
// could disagree with the address: an https URL is TLS for an HTTP protocol, and
// an https:// URL is what turns it on for gRPC, whose address carries no scheme
// of its own. A bare host:port is plaintext, which is what a collector on the
// same host or the same private network wants.
func (c Config) CollectorSecure() bool {
	parsed, err := url.Parse(c.OTEL.Endpoint)
	return err == nil && parsed.Scheme == "https"
}

// Compression names OTEL.Compression accepts.
const (
	// OTELCompressionGzip compresses every export. It is the protocol's own
	// default and what a collector across a network wants.
	OTELCompressionGzip = "gzip"
	// OTELCompressionNone sends the payload uncompressed, which saves the CPU
	// when the collector is on the same host or the same local network.
	OTELCompressionNone = "none"
)

// OTELCompressions returns every accepted compression name.
func OTELCompressions() []string {
	return []string{OTELCompressionGzip, OTELCompressionNone}
}

// Log transport names, the values Log.Transport accepts. Each one is a sink the
// logger builds; naming it in the list is what switches it on.
const (
	// LogTransportConsole writes to the terminal. It is the default and the
	// only sink a fresh checkout needs.
	LogTransportConsole = "console"
	// LogTransportFile writes one JSON object per line to a rotating file under
	// storage.local_path + /logs.
	LogTransportFile = "file"
	// LogTransportOTLP ships entries to an OpenTelemetry collector.
	LogTransportOTLP = "otlp"
)

// LogTransports returns every accepted transport name, in the order the
// documentation lists them. It is what a validation message names, so a rejected
// value is answered with the list rather than with one example.
func LogTransports() []string {
	return []string{LogTransportConsole, LogTransportFile, LogTransportOTLP}
}
