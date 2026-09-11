package config

func setDefaults() {
	v.SetDefault("host", "localhost")
	v.SetDefault("port", 3080)
	v.SetDefault("app.mode", "development")
	v.SetDefault("app.log_level", "info")
	v.SetDefault("app.log_transport", "console")
	v.SetDefault("app.log_format", "structured")
	v.SetDefault("app.log_file", "storage/logs/app.log")
	v.SetDefault("auth.access_token_expiry", 900)
	v.SetDefault("auth.refresh_token_expiry", 7200)
	v.SetDefault("database.url", "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	v.SetDefault("mailer.from_email", "mailer@example.com")
	v.SetDefault("mailer.from_name", "MyApplication")
	v.SetDefault("mailer.smtp_host", "localhost")
	v.SetDefault("mailer.smtp_port", 1025)
	v.SetDefault("mailer.smtp_secure", false)
	v.SetDefault("public.base_url", "http://localhost:3000")
	v.SetDefault("public.healthcheck_url", "https://api.ipify.org")
	v.SetDefault("public.s3_assets_url", "http://localhost:9180")
	v.SetDefault("public.jwt_access_token_expiry", 900)
	v.SetDefault("public.jwt_refresh_token_expiry", 7200)
	v.SetDefault("public.rate_limit_default_max", 100)
	v.SetDefault("public.rate_limit_default_window", 900)
	v.SetDefault("storage.max_upload_size", 5242880)
	v.SetDefault("storage.s3.bucket_default", "devbucket")
	v.SetDefault("storage.s3.endpoint_url", "http://localhost:9100")
	v.SetDefault("storage.s3.force_path_style", true)
	v.SetDefault("storage.s3.region", "auto")
	v.SetDefault("storage.s3.signed_url_expires", 3600)
}

func bindEnvVars() {
	bindings := map[string]string{
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

	for key, env := range bindings {
		_ = v.BindEnv(key, env)
	}
}
