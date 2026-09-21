package config

import "time"

// DefaultDataDir is where the application keeps local files, relative to the
// working directory. It is the default of Storage.LocalPath and of the storage
// health check, and it matches the compose volume (./storage:/srv/storage).
const DefaultDataDir = "storage"

// LogDir is the subdirectory of Storage.LocalPath the file sink writes to. It is
// a subdirectory rather than the data directory itself so the rotating files sit
// beside the backups and the certificates instead of among them.
const LogDir = "logs"

// LogFileName is the name of the active log file, inside LogDir. The rotated
// files take a timestamp suffix from it.
const LogFileName = "tango.log"

// DefaultS3Region is the signing region a deployment that never sets one gets.
//
// A region cannot be empty: the S3 client refuses to resolve an endpoint without
// one and every request fails, even against a service that ignores the region
// such as MinIO. The value is therefore a usable one rather than a placeholder,
// and a deployment with a real region replaces it.
const DefaultS3Region = "us-east-1"

// DefaultOTLPEndpoint is the address a local OpenTelemetry collector listens on.
// It is the protocol's own default port, so one endpoint reaches a collector
// started on the same host. It is shared by logs, traces, and metrics: a
// collector is one endpoint receiving three signals.
const DefaultOTLPEndpoint = "http://localhost:4318"

// DefaultOTELGRPCPort is the port a gRPC collector listens on. It is the
// protocol's own default, and it is why a gRPC address must not be left at the
// HTTP default port: the two protocols listen on different ports of the same
// collector, so flipping otel.protocol without the address moves nothing.
const DefaultOTELGRPCPort = "4317"

// DefaultOTELHTTPPort is the port the default endpoint belongs to, and the one a
// gRPC configuration is refused at.
const DefaultOTELHTTPPort = "4318"

// DefaultOTELQueueSize is how many items one signal buffers before it starts
// dropping. The queue is what keeps export off the request path, so it is sized
// to absorb a collector that is briefly down rather than to a minimum.
const DefaultOTELQueueSize = 4096

// DefaultOTELBatchTimeout is how long a span waits in the queue before it is
// shipped, and DefaultOTELMaxBatchSize is how many go in one export. Together
// they trade export frequency against payload size.
const (
	DefaultOTELBatchTimeout = 5 * time.Second
	DefaultOTELMaxBatchSize = 512
)

// DefaultOTELExportTimeout bounds one export attempt for every signal. It is
// shorter than the batch interval so a stalled collector cannot make the export
// goroutine fall behind its own schedule.
const DefaultOTELExportTimeout = 10 * time.Second

// DefaultOTELMetricInterval is how often measurements are handed to the
// exporter. It is longer than the trace batch because a metric export carries
// the state of every instrument at once, not one event.
const DefaultOTELMetricInterval = 60 * time.Second

// DefaultPrometheusPath is where the Prometheus exposition is served on the
// application's own port. It is the path a scraper looks for by convention, so
// a scrape job needs no configuration beyond the address.
const DefaultPrometheusPath = "/metrics"

// DefaultCORSMaxAge is how long a browser may cache a preflight answer. An hour
// keeps the preflight out of most sessions without promising an origin list a
// redeploy of the configuration could have changed.
const DefaultCORSMaxAge = time.Hour

// DefaultCORSOrigins is the origin list a fresh checkout gets: the Vite dev
// server the SPA is served from in development. Production names its own
// origin through the configuration, so a browser has to prove where the call
// comes from rather than being trusted by default.
var DefaultCORSOrigins = []string{"http://localhost:3000"}

// DefaultCORSMethods is the method list a REST + ConnectRPC surface needs.
var DefaultCORSMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// DefaultCORSHeaders is the header list a browser call may set: the fetch
// headers the API envelope reads plus the authorization header the tokens
// arrive in.
var DefaultCORSHeaders = []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"}

// Mode names of the supported runtime modes.
const (
	ModeDevelopment = "development"
	ModeStaging     = "staging"
	ModeProduction  = "production"
	ModeTest        = "test"
)

// Log levels accepted by Log.Level.
const (
	LogDebug = "debug"
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"
)

// Defaults for the rotating file sink. The sink is opt-in, so these apply only
// once log.transport names it.
const (
	// DefaultLogMaxSizeMB is the size at which the active file is rotated. It
	// matches the sink's own default, so leaving the key out and writing 100
	// produce the same file.
	DefaultLogMaxSizeMB = 100
	// DefaultLogMaxBackups and DefaultLogMaxAge bound what is kept. Both are
	// needed: they are independent limits, and zero on both would keep every
	// rotated file forever, which fills a disk quietly.
	DefaultLogMaxBackups = 7
	DefaultLogMaxAge     = 30
)

// Default returns the built-in configuration. These values are the lowest
// precedence layer: every other source may replace them, but a key no source
// mentions keeps the value set here.
func Default() Config {
	return Config{
		App: App{
			Mode: ModeDevelopment,
		},
		Auth: Auth{
			Issuer:     "tango",
			AccessTTL:  15 * time.Minute,
			RefreshTTL: 30 * 24 * time.Hour,
		},
		Cache: Cache{
			Driver: CacheMemory,
			TTL:    5 * time.Minute,
		},
		Database: Database{
			MaxConns:        10,
			MinConns:        2,
			MaxConnLifetime: time.Hour,
			MaxConnIdleTime: 30 * time.Minute,
			ConnectTimeout:  5 * time.Second,
			SearchPath:      "public,internal,reference",
			Timezone:        "UTC",
		},
		KVStore: KVStore{
			// Disabled by default, so a fresh checkout runs on Postgres and
			// in-process memory alone. The URL points at the server compose
			// starts, so switching Enable on is the only step needed.
			Enable: false,
			URL:    "redis://default:securedb@localhost:6379",
			DB:     0,
		},
		Log: Log{
			Level:  LogInfo,
			Format: LogPretty,
			// The console alone: a fresh checkout writes to the terminal and
			// nothing else, so no run needs a volume or a collector to start.
			Transport: []string{LogTransportConsole},
			File: LogFile{
				MaxSize:    DefaultLogMaxSizeMB,
				MaxBackups: DefaultLogMaxBackups,
				MaxAge:     DefaultLogMaxAge,
				Compress:   true,
			},
			// A ten-second export attempt, shared with the other signals:
			// DefaultOTELExportTimeout is the one answer to "how long may one
			// export hold".
			OTLP: LogOTLP{Timeout: DefaultOTELExportTimeout},
		},
		OTEL: OTEL{
			// The collector a local receiver listens on, so enabling a signal
			// is the only step needed.
			Endpoint: DefaultOTLPEndpoint,
			// HTTP/protobuf, the specification's own default and the one the
			// default endpoint's port belongs to. It is also the protocol an
			// HTTP deployment can proxy.
			Protocol:    OTELProtocolHTTPProtobuf,
			ServiceName: AppIdentifier,
			Compression: OTELCompressionGzip,
			// No headers: a collector that checks nothing needs nothing sent.
			Headers: map[string]string{},
			Queue: OTELQueue{
				MaxSize: DefaultOTELQueueSize,
			},
			// No path on any signal: a collector serves the protocol's own
			// routes, and the shared endpoint already names where it is.
			Tracing: OTELTracing{
				// Disabled: a signal nothing consumes is work nothing asked
				// for. The sampler records everything once it is switched on,
				// which is what a developer wiring the stack wants to see.
				Enable:        false,
				Sampler:       OTELSamplerAlways,
				Ratio:         1,
				BatchTimeout:  DefaultOTELBatchTimeout,
				ExportTimeout: DefaultOTELExportTimeout,
				MaxBatchSize:  DefaultOTELMaxBatchSize,
			},
			Metrics: OTELMetrics{
				Enable:         false,
				PrometheusPath: DefaultPrometheusPath,
				Interval:       DefaultOTELMetricInterval,
				ExportTimeout:  DefaultOTELExportTimeout,
			},
		},
		Mailer: Mailer{
			FromEmail: "mailer@example.com",
			FromName:  "Tango Mailer",
			SMTPPort:  587,
		},
		RateLimit: RateLimit{
			Driver: RateLimitDB,
			Limit:  60,
			Window: time.Minute,
		},
		Server: Server{
			Host:            "0.0.0.0",
			Port:            3080,
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 15 * time.Second,
			CORS: CORS{
				AllowedOrigins: DefaultCORSOrigins,
				AllowedMethods: DefaultCORSMethods,
				AllowedHeaders: DefaultCORSHeaders,
				MaxAge:         DefaultCORSMaxAge,
			},
		},
		Session: Session{
			Driver: SessionDB,
			TTL:    7 * 24 * time.Hour,
		},
		Storage: Storage{
			Driver:    StorageLocal,
			LocalPath: DefaultDataDir,
			S3: S3{
				// Disabled by default along with the driver, but the values
				// point at the service compose starts, so switching the driver
				// is the only step needed.
				ForcePathStyle:   true,
				Region:           DefaultS3Region,
				SignedURLExpires: time.Hour,
			},
		},
	}
}

// DefaultsMap returns the built-in defaults as a flat map keyed by config path.
// A secret has an empty default, so a fresh checkout carries no placeholder
// credential: the layer that supplies it is the only source of that key.
func DefaultsMap() map[string]any {
	return flatten(Default())
}
