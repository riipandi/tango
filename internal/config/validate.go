package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Validate checks the semantic rules the loader cannot express as
// defaults: enums, ranges, URL shapes, and the secrets that are
// mandatory in production. It runs at the end of Load, so every
// consumer — serve, migrate, health — fails fast on a broken
// configuration instead of deep inside a request.
func (c *Config) Validate() error {
	errs := []error{
		validateRange("port", c.Port, 1, 65535),
		validateEnum("app.mode", c.App.Mode, "development", "production"),
		validateEnum("app.log_level", c.App.LogLevel, "trace", "debug", "info", "warn", "error", "fatal", "panic"),
		validateEnum("app.log_transport", c.App.LogTransport, "console", "file"),
		validateEnum("app.log_format", c.App.LogFormat, "pretty", "structured"),
		validateURL("database.url", c.Database.URL, "postgres", "postgresql"),
		validateURL("public.base_url", c.Public.BaseURL, "http", "https"),
		validateURL("public.s3_assets_url", c.Public.S3AssetsURL, "http", "https"),
		validateURL("public.healthcheck_url", c.Public.HealthcheckURL, "http", "https"),
	}

	if c.App.Mode == "production" {
		for _, secret := range []struct{ key, value string }{
			{"app.secret_key", c.App.SecretKey},
			{"auth.secret_key", c.Auth.SecretKey},
			{"auth.private_key", c.Auth.PrivateKey},
			{"auth.public_key", c.Auth.PublicKey},
		} {
			if secret.value == "" {
				errs = append(errs, fmt.Errorf("%s: required in production", secret.key))
			}
		}
	}

	return errors.Join(errs...)
}

func validateEnum(name, value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s: invalid value %q (want one of %s)", name, value, strings.Join(allowed, ", "))
}

func validateRange(name string, value, min, max int) error {
	if value < min || value > max {
		return fmt.Errorf("%s: %d out of range [%d, %d]", name, value, min, max)
	}
	return nil
}

func validateURL(name, raw string, schemes ...string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for _, scheme := range schemes {
		if parsed.Scheme == scheme {
			return nil
		}
	}
	return fmt.Errorf("%s: unsupported scheme %q (want %s)", name, parsed.Scheme, strings.Join(schemes, ", "))
}
