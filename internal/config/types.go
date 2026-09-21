package config

import "time"

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
	Log       Log       `koanf:"log" json:"log"`
	RateLimit RateLimit `koanf:"rate_limit" json:"rate_limit"`
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
	// Driver is CacheMemory or CacheKV.
	Driver string `koanf:"driver" json:"driver"`
	// TTL is the default lifetime of a cached entry.
	TTL time.Duration `koanf:"ttl" json:"ttl"`
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

// Log holds the logging settings.
type Log struct {
	// Level is one of debug, info, warn, or error.
	Level string `koanf:"level" json:"level"`
	// Format is LogPretty or LogStructured.
	Format string `koanf:"format" json:"format"`
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
	// BaseURL is the public origin, used to build absolute links.
	BaseURL string `koanf:"base_url" json:"base_url"`
	// ReadTimeout, WriteTimeout, and IdleTimeout are the net/http timeouts.
	ReadTimeout  time.Duration `koanf:"read_timeout" json:"read_timeout"`
	WriteTimeout time.Duration `koanf:"write_timeout" json:"write_timeout"`
	IdleTimeout  time.Duration `koanf:"idle_timeout" json:"idle_timeout"`
	// ShutdownTimeout bounds the graceful shutdown drain.
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout" json:"shutdown_timeout"`
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
