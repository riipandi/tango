//go:build !debug

package launcher

// secretsPlugins is empty in release builds: the secrets command is
// a development tool and stays out of the release binary.
func secretsPlugins() []any { return nil }
