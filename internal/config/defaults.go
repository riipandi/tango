package config

// defaultConfig is the bottom Load layer: one struct, so defaults
// can't drift from the schema.
var defaultConfig = Config{
	Host: "localhost",
	Port: 3080,

	App: AppConfig{
		Mode:         "development",
		DataDir:      "storage",
		LogLevel:     "info",
		LogTransport: "file",
		LogFormat:    "structured",
	},

	Auth: AuthConfig{
		AccessTokenExpiry:  900,
		RefreshTokenExpiry: 7200,
		SessionLifetime:    2592000,
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

	Queue: QueueConfig{
		Workers:         4,
		ReleaseAfter:    300,
		CleanupInterval: 21600,
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
