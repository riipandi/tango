package config

type Config struct {
	Host     string         `mapstructure:"host"`
	Port     int            `mapstructure:"port"`
	App      AppConfig      `mapstructure:"app"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Database DatabaseConfig `mapstructure:"database"`
	Mailer   MailerConfig   `mapstructure:"mailer"`
	Public   PublicConfig   `mapstructure:"public"`
	Storage  StorageConfig  `mapstructure:"storage"`
}

type AppConfig struct {
	Mode         string `mapstructure:"mode"`
	LogLevel     string `mapstructure:"log_level"`
	LogTransport string `mapstructure:"log_transport"`
	SecretKey    string `mapstructure:"secret_key"`
}

type AuthConfig struct {
	PrivateKey         string `mapstructure:"private_key"`
	PublicKey          string `mapstructure:"public_key"`
	SecretKey          string `mapstructure:"secret_key"`
	AccessTokenExpiry  int    `mapstructure:"access_token_expiry"`
	RefreshTokenExpiry int    `mapstructure:"refresh_token_expiry"`
	GithubClientID     string `mapstructure:"github_client_id"`
	GithubClientSecret string `mapstructure:"github_client_secret"`
	GoogleClientID     string `mapstructure:"google_client_id"`
	GoogleClientSecret string `mapstructure:"google_client_secret"`
}

type DatabaseConfig struct {
	URL string `mapstructure:"url"`
}

type MailerConfig struct {
	FromEmail    string `mapstructure:"from_email"`
	FromName     string `mapstructure:"from_name"`
	SMTPHost     string `mapstructure:"smtp_host"`
	SMTPPort     int    `mapstructure:"smtp_port"`
	SMTPUsername string `mapstructure:"smtp_username"`
	SMTPPassword string `mapstructure:"smtp_password"`
	SMTPSecure   bool   `mapstructure:"smtp_secure"`
}

type PublicConfig struct {
	BaseURL                string   `mapstructure:"base_url"`
	S3AssetsURL            string   `mapstructure:"s3_assets_url"`
	TrustedOrigins         []string `mapstructure:"trusted_origins"`
	JwtAccessTokenExpiry   int      `mapstructure:"jwt_access_token_expiry"`
	JwtRefreshTokenExpiry  int      `mapstructure:"jwt_refresh_token_expiry"`
	RateLimitDefaultMax    int      `mapstructure:"rate_limit_default_max"`
	RateLimitDefaultWindow int      `mapstructure:"rate_limit_default_window"`
	HealthcheckURL         string   `mapstructure:"healthcheck_url"`
}

type StorageConfig struct {
	MaxUploadSize int64    `mapstructure:"max_upload_size"`
	S3            S3Config `mapstructure:"s3"`
}

type S3Config struct {
	AccessKeyID      string  `mapstructure:"access_key_id"`
	BucketDefault    string  `mapstructure:"bucket_default"`
	EndpointURL      string  `mapstructure:"endpoint_url"`
	ForcePathStyle   bool    `mapstructure:"force_path_style"`
	PathPrefix       *string `mapstructure:"path_prefix"`
	Region           string  `mapstructure:"region"`
	SecretAccessKey  string  `mapstructure:"secret_access_key"`
	SignedURLExpires int     `mapstructure:"signed_url_expires"`
}
