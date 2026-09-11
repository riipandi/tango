package config

// Config is the runtime configuration. It is the schema AND the
// source of its own defaults (see defaultConfig): koanf tags name
// the keys, the defaultConfig literal fills the bottom layer.
//
// Environment variables map mechanically: the first underscore-
// separated segment selects the section (AUTH_PRIVATE_KEY ->
// auth.private_key) and the rest keeps its snake_case form.
type Config struct {
	Host     string         `koanf:"host"`
	Port     int            `koanf:"port"`
	App      AppConfig      `koanf:"app"`
	Auth     AuthConfig     `koanf:"auth"`
	Database DatabaseConfig `koanf:"database"`
	Mailer   MailerConfig   `koanf:"mailer"`
	Public   PublicConfig   `koanf:"public"`
	Storage  StorageConfig  `koanf:"storage"`
}

type AppConfig struct {
	Mode         string `koanf:"mode"`
	LogLevel     string `koanf:"log_level"`
	LogTransport string `koanf:"log_transport"`
	LogFormat    string `koanf:"log_format"`
	LogFile      string `koanf:"log_file"`
	SecretKey    string `koanf:"secret_key"`
}

type AuthConfig struct {
	PrivateKey         string `koanf:"private_key"`
	PublicKey          string `koanf:"public_key"`
	SecretKey          string `koanf:"secret_key"`
	AccessTokenExpiry  int    `koanf:"access_token_expiry"`
	RefreshTokenExpiry int    `koanf:"refresh_token_expiry"`
	GithubClientID     string `koanf:"github_client_id"`
	GithubClientSecret string `koanf:"github_client_secret"`
	GoogleClientID     string `koanf:"google_client_id"`
	GoogleClientSecret string `koanf:"google_client_secret"`
}

type DatabaseConfig struct {
	URL string `koanf:"url"`
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
	BaseURL                string   `koanf:"base_url"`
	S3AssetsURL            string   `koanf:"s3_assets_url"`
	TrustedOrigins         []string `koanf:"trusted_origins"`
	JwtAccessTokenExpiry   int      `koanf:"jwt_access_token_expiry"`
	JwtRefreshTokenExpiry  int      `koanf:"jwt_refresh_token_expiry"`
	RateLimitDefaultMax    int      `koanf:"rate_limit_default_max"`
	RateLimitDefaultWindow int      `koanf:"rate_limit_default_window"`
}

type StorageConfig struct {
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
