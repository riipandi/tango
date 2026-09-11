//go:build !debug

package launcher

import "github.com/alecthomas/kong"

// secretsOptions registers nothing in release builds: the secrets
// command is a development tool and stays out of the release binary.
func secretsOptions() []kong.Option { return nil }
