package config

// defaultConfig is the base config layer and schema source for defaults.
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
		AccessTokenExpiry:    600,
		SessionLifetime:      2592000,
		SessionShortLifetime: 43200,
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

	OIDC: OIDCConfig{
		AccessTokenExpiry:       3600,
		RefreshTokenExpiry:      2592000,
		AuthorizationCodeExpiry: 120,
		InteractionExpiry:       900,
		DeviceCodeExpiry:        900,
		PARExpiry:               60,
	},

	Public: PublicConfig{
		BaseURL:         "http://localhost:3000",
		S3AssetsURL:     "http://localhost:9180",
		VersionCheckURL: "https://api.github.com/repos/riipandi/tango/releases/latest",
	},

	Queue: QueueConfig{
		Workers:         4,
		ReleaseAfter:    300,
		CleanupInterval: 21600,
	},

	Storage: StorageConfig{
		DataDir:            "storage/files",
		MaxUploadSize:      5242880,
		S3BucketDefault:    "devbucket",
		S3EndpointURL:      "http://localhost:9100",
		S3ForcePathStyle:   true,
		S3Region:           "auto",
		S3SignedURLExpires: 3600,
	},
}
