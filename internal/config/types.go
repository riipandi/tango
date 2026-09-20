package config

// Config defines runtime settings and their koanf keys.
type Config struct {
	Host     string         `koanf:"host"`
	Port     int            `koanf:"port"`
	App      AppConfig      `koanf:"app"`
	Auth     AuthConfig     `koanf:"auth"`
	Database DatabaseConfig `koanf:"database"`
	Mailer   MailerConfig   `koanf:"mailer"`
	OIDC     OIDCConfig     `koanf:"oidc"`
	Public   PublicConfig   `koanf:"public"`
	Queue    QueueConfig    `koanf:"queue"`
	Storage  StorageConfig  `koanf:"storage"`
}

type AppConfig struct {
	Mode         string `koanf:"mode"`
	DataDir      string `koanf:"data_dir"`
	LogLevel     string `koanf:"log_level"`
	LogTransport string `koanf:"log_transport"`
	LogFormat    string `koanf:"log_format"`
	SecretKey    string `koanf:"secret_key"`
}

// AuthConfig holds the internal authentication lifetimes. Every
// duration is in seconds, matching the rest of the environment
// surface.
type AuthConfig struct {
	PrivateKey string `koanf:"private_key"`
	PublicKey  string `koanf:"public_key"`
	SecretKey  string `koanf:"secret_key"`

	// AccessTokenExpiry bounds the internal bearer JWT the SPA sends
	// on RPC calls; refresh stays cookie-only.
	AccessTokenExpiry int `koanf:"access_token_expiry"`
	// SessionLifetime bounds a session issued with "remember me".
	SessionLifetime int `koanf:"session_lifetime"`
	// SessionShortLifetime bounds a session issued without "remember
	// me"; it must not exceed SessionLifetime.
	SessionShortLifetime int `koanf:"session_short_lifetime"`

	GithubClientID     string `koanf:"github_client_id"`
	GithubClientSecret string `koanf:"github_client_secret"`
	GoogleClientID     string `koanf:"google_client_id"`
	GoogleClientSecret string `koanf:"google_client_secret"`
}

type DatabaseConfig struct {
	URL string `koanf:"url"`
}

// OIDCConfig holds the relying-party protocol lifetimes. A client's
// own access/refresh durations override the two token defaults.
type OIDCConfig struct {
	// AccessTokenExpiry and RefreshTokenExpiry are the provider
	// defaults for clients without their own durations.
	AccessTokenExpiry  int `koanf:"access_token_expiry"`
	RefreshTokenExpiry int `koanf:"refresh_token_expiry"`
	// AuthorizationCodeExpiry bounds the one-time code lifetime.
	AuthorizationCodeExpiry int `koanf:"authorization_code_expiry"`
	// InteractionExpiry bounds the sign-in/consent bridge.
	InteractionExpiry int `koanf:"interaction_expiry"`
	// DeviceCodeExpiry bounds the device authorization window
	// (RFC 8628 6.1).
	DeviceCodeExpiry int `koanf:"device_code_expiry"`
	// PARExpiry bounds a pushed authorization request_uri lifetime
	// (RFC 9126 3.2.2 recommends keeping it short).
	PARExpiry int `koanf:"par_expiry"`
}

type MailerConfig struct {
	FromEmail    string `koanf:"from_email"`
	FromName     string `koanf:"from_name"`
	SMTPHost     string `koanf:"smtp_host"`
	SMTPPort     int    `koanf:"smtp_port"`
	SMTPUsername string `koanf:"smtp_username"`
	SMTPPassword string `koanf:"smtp_password"`
	SMTPSecure   bool   `koanf:"smtp_secure"`
}

type PublicConfig struct {
	BaseURL         string   `koanf:"base_url"`
	S3AssetsURL     string   `koanf:"s3_assets_url"`
	VersionCheckURL string   `koanf:"version_check_url"`
	TrustedOrigins  []string `koanf:"trusted_origins"`
}

type QueueConfig struct {
	Workers         int `koanf:"workers"`
	ReleaseAfter    int `koanf:"release_after"`
	CleanupInterval int `koanf:"cleanup_interval"`
}

type StorageConfig struct {
	DataDir            string  `koanf:"data_dir"`
	MaxUploadSize      int64   `koanf:"max_upload_size"`
	S3AccessKeyID      string  `koanf:"s3_access_key_id"`
	S3BucketDefault    string  `koanf:"s3_bucket_default"`
	S3EndpointURL      string  `koanf:"s3_endpoint_url"`
	S3ForcePathStyle   bool    `koanf:"s3_force_path_style"`
	S3PathPrefix       *string `koanf:"s3_path_prefix"`
	S3Region           string  `koanf:"s3_region"`
	S3SecretAccessKey  string  `koanf:"s3_secret_access_key"`
	S3SignedURLExpires int     `koanf:"s3_signed_url_expires"`
}
