package config

import "time"

// DefaultDataDir is where the application keeps local files, relative to the
// working directory. It is the default of Storage.LocalPath and of the storage
// health check, and it matches the compose volume (./storage:/srv/storage).
const DefaultDataDir = "storage"

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
		Log: Log{
			Level:  LogInfo,
			Format: LogPretty,
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
		},
	}
}

// DefaultsMap returns the built-in defaults as a flat map keyed by config path.
// A secret has an empty default, so a fresh checkout carries no placeholder
// credential: the layer that supplies it is the only source of that key.
func DefaultsMap() map[string]any {
	return flatten(Default())
}
