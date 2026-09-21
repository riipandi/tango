package config

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Default returns the built-in configuration. These values are the lowest
// precedence layer: every other source may replace them, but a key no source
// mentions keeps the value set here.
func Default() Config {
	return Config{
		App: App{
			Env:     EnvDevelopment,
			DataDir: DefaultDataDir,
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
			Format: LogText,
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

// Env names of the supported runtime environments.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
	EnvTest        = "test"
)

// Log levels accepted by Log.Level.
const (
	LogDebug = "debug"
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"
)

// DefaultsMap returns the built-in defaults as a flat map keyed by config path.
// A secret has an empty default, so a fresh checkout carries no placeholder
// credential: the layer that supplies it is the only source of that key.
func DefaultsMap() map[string]any {
	return flatten(Default())
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
