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
// It is the protocol's own default port, so an enable flag alone reaches a
// collector started on the same host.
const DefaultOTLPEndpoint = "http://localhost:4318"

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
			OTLP: LogOTLP{
				// The collector a local OTLP receiver listens on, so naming the
				// transport is the only step needed.
				Endpoint: DefaultOTLPEndpoint,
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
