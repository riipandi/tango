package config

// redacted is the placeholder a Redacted Config prints instead of a secret.
const redacted = "[redacted]"

// Redacted returns a copy with every secret replaced by a placeholder, safe to
// print or log. The connection string is reduced to its host and database, so a
// report can name the target without leaking the password.
func (c Config) Redacted() Config {
	out := c
	out.App.SecretKey = redacted
	out.Auth.PrivateKey = redacted
	out.Auth.PublicKey = redacted
	out.Auth.SecretKey = redacted
	out.Database.URL = RedactDSN(c.Database.URL)
	out.origin = nil
	return out
}
