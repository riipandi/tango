package config

// defaultsMap returns the built-in defaults as flat dotted keys —
// the bottom layer of the Load pipeline.
func defaultsMap() map[string]any {
	return map[string]any{
		"host": "localhost",
		"port": 3080,

		"app.mode":          "development",
		"app.log_level":     "info",
		"app.log_transport": "console",
		"app.log_format":    "structured",
		"app.log_file":      "storage/logs/app.log",

		"auth.access_token_expiry":  900,
		"auth.refresh_token_expiry": 7200,

		"database.url": "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable",

		"mailer.from_email":  "mailer@example.com",
		"mailer.from_name":   "MyApplication",
		"mailer.smtp_host":   "localhost",
		"mailer.smtp_port":   1025,
		"mailer.smtp_secure": false,

		"public.base_url":                  "http://localhost:3000",
		"public.healthcheck_url":           "https://api.ipify.org",
		"public.s3_assets_url":             "http://localhost:9180",
		"public.jwt_access_token_expiry":   900,
		"public.jwt_refresh_token_expiry":  7200,
		"public.rate_limit_default_max":    100,
		"public.rate_limit_default_window": 900,

		"storage.max_upload_size":       5242880,
		"storage.s3.bucket_default":     "devbucket",
		"storage.s3.endpoint_url":       "http://localhost:9100",
		"storage.s3.force_path_style":   true,
		"storage.s3.region":             "auto",
		"storage.s3.signed_url_expires": 3600,
	}
}

// envBindings maps config keys to their system environment variable
// names. It is the single source of truth for environment loading;
// unbound variables are ignored.
var envBindings = map[string]string{
	"host":                             "HOST",
	"port":                             "PORT",
	"app.mode":                         "APP_MODE",
	"app.log_level":                    "APP_LOG_LEVEL",
	"app.log_transport":                "APP_LOG_TRANSPORT",
	"app.log_format":                   "APP_LOG_FORMAT",
	"app.log_file":                     "APP_LOG_FILE",
	"app.secret_key":                   "APP_SECRET_KEY",
	"auth.private_key":                 "AUTH_PRIVATE_KEY",
	"auth.public_key":                  "AUTH_PUBLIC_KEY",
	"auth.secret_key":                  "AUTH_SECRET_KEY",
	"auth.access_token_expiry":         "AUTH_ACCESS_TOKEN_EXPIRY",
	"auth.refresh_token_expiry":        "AUTH_REFRESH_TOKEN_EXPIRY",
	"auth.github_client_id":            "AUTH_GITHUB_CLIENT_ID",
	"auth.github_client_secret":        "AUTH_GITHUB_CLIENT_SECRET",
	"auth.google_client_id":            "AUTH_GOOGLE_CLIENT_ID",
	"auth.google_client_secret":        "AUTH_GOOGLE_CLIENT_SECRET",
	"database.url":                     "DATABASE_URL",
	"mailer.from_email":                "MAILER_FROM_EMAIL",
	"mailer.from_name":                 "MAILER_FROM_NAME",
	"mailer.smtp_host":                 "MAILER_SMTP_HOST",
	"mailer.smtp_port":                 "MAILER_SMTP_PORT",
	"mailer.smtp_username":             "MAILER_SMTP_USERNAME",
	"mailer.smtp_password":             "MAILER_SMTP_PASSWORD",
	"mailer.smtp_secure":               "MAILER_SMTP_SECURE",
	"public.base_url":                  "PUBLIC_BASE_URL",
	"public.healthcheck_url":           "PUBLIC_HEALTHCHECK_URL",
	"public.s3_assets_url":             "PUBLIC_S3_ASSETS_URL",
	"public.trusted_origins":           "PUBLIC_TRUSTED_ORIGINS",
	"public.jwt_access_token_expiry":   "PUBLIC_JWT_ACCESS_TOKEN_EXPIRY",
	"public.jwt_refresh_token_expiry":  "PUBLIC_JWT_REFRESH_TOKEN_EXPIRY",
	"public.rate_limit_default_max":    "PUBLIC_RATE_LIMIT_DEFAULT_MAX",
	"public.rate_limit_default_window": "PUBLIC_RATE_LIMIT_DEFAULT_WINDOW",
	"storage.max_upload_size":          "STORAGE_MAX_UPLOAD_SIZE",
	"storage.s3.access_key_id":         "STORAGE_S3_ACCESS_KEY_ID",
	"storage.s3.bucket_default":        "STORAGE_S3_BUCKET_DEFAULT",
	"storage.s3.endpoint_url":          "STORAGE_S3_ENDPOINT_URL",
	"storage.s3.force_path_style":      "STORAGE_S3_FORCE_PATH_STYLE",
	"storage.s3.path_prefix":           "STORAGE_S3_PATH_PREFIX",
	"storage.s3.region":                "STORAGE_S3_REGION",
	"storage.s3.secret_access_key":     "STORAGE_S3_SECRET_ACCESS_KEY",
	"storage.s3.signed_url_expires":    "STORAGE_S3_SIGNED_URL_EXPIRES",
}
