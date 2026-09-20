package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Validate checks values that defaults cannot express, then reports all errors.
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

		// Lifetimes are seconds; a non-positive value would issue an
		// already-expired token or session.
		validateRange("auth.access_token_expiry", c.Auth.AccessTokenExpiry, 1, maxLifetimeSeconds),
		validateRange("auth.session_lifetime", c.Auth.SessionLifetime, 1, maxLifetimeSeconds),
		validateRange("auth.session_short_lifetime", c.Auth.SessionShortLifetime, 1, maxLifetimeSeconds),
		validateRange("oidc.access_token_expiry", c.OIDC.AccessTokenExpiry, 1, maxLifetimeSeconds),
		validateRange("oidc.refresh_token_expiry", c.OIDC.RefreshTokenExpiry, 1, maxLifetimeSeconds),
		validateRange("oidc.authorization_code_expiry", c.OIDC.AuthorizationCodeExpiry, 1, maxLifetimeSeconds),
		validateRange("oidc.interaction_expiry", c.OIDC.InteractionExpiry, 1, maxLifetimeSeconds),
		validateRange("oidc.device_code_expiry", c.OIDC.DeviceCodeExpiry, 1, maxLifetimeSeconds),
		validateRange("oidc.par_expiry", c.OIDC.PARExpiry, 1, maxLifetimeSeconds),
	}

	// A short session longer than the remembered one would make the
	// "remember me" checkbox shorten the session instead of extending
	// it.
	if c.Auth.SessionShortLifetime > c.Auth.SessionLifetime {
		errs = append(errs, fmt.Errorf(
			"auth.session_short_lifetime: %d exceeds auth.session_lifetime (%d)",
			c.Auth.SessionShortLifetime, c.Auth.SessionLifetime))
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

// maxLifetimeSeconds bounds every configurable lifetime: one year.
// Anything longer is a misconfiguration, not a preference.
const maxLifetimeSeconds = 365 * 24 * 60 * 60

func validateEnum(name, value string, allowed ...string) error {
	if slices.Contains(allowed, value) {
		return nil
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
	if slices.Contains(schemes, parsed.Scheme) {
		return nil
	}
	return fmt.Errorf("%s: unsupported scheme %q (want %s)", name, parsed.Scheme, strings.Join(schemes, ", "))
}
