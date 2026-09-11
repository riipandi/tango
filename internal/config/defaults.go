package config

// defaultConfig is the bottom layer of the Load pipeline: a plain
// Config value, so defaults can never drift from the schema — one
// struct, one place.
var defaultConfig = Config{
	Host: "localhost",
	Port: 3080,

	App: AppConfig{
		Mode:         "development",
		LogLevel:     "info",
		LogTransport: "file",
		LogFormat:    "structured",
		LogFile:      "storage/logs/app.log",
	},

	Auth: AuthConfig{
		AccessTokenExpiry:  900,
		RefreshTokenExpiry: 7200,
	},

	Database: DatabaseConfig{
		URL: "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable",
	},

	Mailer: MailerConfig{
		FromEmail: "mailer@example.com",
		FromName:  "MyApplication",
		SMTPHost:  "localhost",
		SMTPPort:  1025,
	},

	Public: PublicConfig{
		BaseURL:                "http://localhost:3000",
		S3AssetsURL:            "http://localhost:9180",
		JwtAccessTokenExpiry:   900,
		JwtRefreshTokenExpiry:  7200,
		RateLimitDefaultMax:    100,
		RateLimitDefaultWindow: 900,
	},

	Storage: StorageConfig{
		MaxUploadSize:      5242880,
		S3BucketDefault:    "devbucket",
		S3EndpointURL:      "http://localhost:9100",
		S3ForcePathStyle:   true,
		S3Region:           "auto",
		S3SignedURLExpires: 3600,
	},
}
