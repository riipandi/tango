package config

import (
	"strings"
)

// Delim is the key separator used throughout the config layer. It is the same
// delimiter the JSON file uses for nesting, so a file key and an environment
// variable name resolve to the same config key.
const Delim = "."

// Layer names, lowest precedence first. Load applies them in this order, so a
// later layer wins: the JSON config file replaces a built-in default, the env
// file replaces the system environment, and a command-line flag replaces both.
const (
	LayerDefault    = "default"
	LayerConfigFile = "config-file"
	LayerSystemEnv  = "system-env"
	LayerEnvFile    = "env-file"
	LayerFlag       = "flag"
)

// EnvName returns the environment variable name that sets a config key:
// database.url becomes DATABASE_URL. The mapping is total, so a key is always
// reachable from the environment under exactly one name.
func EnvName(key string) string {
	return strings.ToUpper(strings.ReplaceAll(key, Delim, "_"))
}

// envLayer builds a flat config map from the system environment, keeping only
// the variables that name a known config key.
func envLayer(environ []string) map[string]any {
	return pickEnv(environ, func(name string) (string, bool) { return name, true })
}

// envFileLayer maps the variables of a dotenv file to config keys. A name that
// does not match a key is dropped, the same rule the system environment follows.
func envFileLayer(values map[string]string) map[string]any {
	entries := make([]string, 0, len(values))
	for name, value := range values {
		entries = append(entries, name+"="+value)
	}
	return pickEnv(entries, func(name string) (string, bool) { return name, true })
}

// pickEnv keeps the variables that name a known config key. The mapping is
// explicit rather than derived from the variable name, because a config key may
// contain an underscore: deriving it would turn AUTH_SECRET_KEY into
// auth.secret.key, which is not a key at all.
func pickEnv(environ []string, accept func(string) (string, bool)) map[string]any {
	known := envKeyMap()
	out := make(map[string]any)
	for _, entry := range environ {
		name, value, found := strings.Cut(entry, "=")
		if !found || name == "" {
			continue
		}
		name, ok := accept(name)
		if !ok {
			continue
		}
		key, ok := known[name]
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}

// envKeyMap maps an environment variable name to its config key. It is built
// from the struct, so it cannot drift from the fields.
func envKeyMap() map[string]string {
	keys := knownKeys()
	out := make(map[string]string, len(keys))
	for key := range keys {
		out[EnvName(key)] = key
	}
	return out
}

// lookup reads a variable from an environment slice.
func lookup(environ []string, name string) (string, bool) {
	prefix := name + "="
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, prefix); ok {
			return value, true
		}
	}
	return "", false
}

// lookupValue reads a variable from an environment slice, returning "" when it
// is absent.
func lookupValue(environ []string, name string) string {
	value, _ := lookup(environ, name)
	return value
}
