package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"encoding/json/v2"
)

// DefaultConfigFile is the config file Load reads when no source names one and
// the file exists in the working directory. A missing default file is not an
// error: a fresh checkout has none and must still run.
const DefaultConfigFile = "app.config.json"

// FileEnv is the environment variable naming the JSON config file. It is
// overridden by an explicit --config-file flag.
const FileEnv = "CONFIG_FILE"

// ErrNoConfigFile reports a config file that was named but does not exist.
var ErrNoConfigFile = errors.New("config: config file not found")

// ErrUnresolvedVar reports an interpolation directive naming a variable that is
// not set. An empty value is a valid value; an absent variable is not, because
// it would silently blank a key.
var ErrUnresolvedVar = errors.New("config: unresolved variable")

// configFileLayer reads the JSON config file and returns its keys, flattened and
// with interpolation resolved.
//
// The path comes from the flag, then the environment, then the default file in
// the working directory. A path the user named must exist; the default file may
// be absent.
func configFileLayer(opts Options, environ []string) (map[string]any, error) {
	path, required := opts.ConfigFile, true
	if path == "" {
		path, required = lookupValue(environ, FileEnv), true
	}
	if path == "" {
		path, required = DefaultConfigFile, false
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return nil, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoConfigFile, path)
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	flat := flattenNested(doc, nil)
	for key, value := range flat {
		text, ok := value.(string)
		if !ok {
			continue
		}
		resolved, err := interpolate(text, environ)
		if err != nil {
			return nil, fmt.Errorf("config: %s: key %s: %w", path, key, err)
		}
		flat[key] = resolved
	}
	return filterKnown(flat), nil
}

// interpolate expands the two directives a config file may use: env:NAME, the
// whole value, and ${NAME}, inline. Both resolve from the environment, so a
// value can reference a secret that never appears in the file.
//
// A bare $NAME is left alone: it is a common character in a password or a URL,
// and expanding it would corrupt a value the user meant literally.
func interpolate(value string, environ []string) (string, error) {
	if name, ok := strings.CutPrefix(value, "env:"); ok {
		if name == "" {
			return "", fmt.Errorf("%w: env: with no name", ErrUnresolvedVar)
		}
		resolved, found := lookup(environ, name)
		if !found {
			return "", fmt.Errorf("%w: %s", ErrUnresolvedVar, name)
		}
		return resolved, nil
	}

	if !strings.Contains(value, "${") {
		return value, nil
	}
	return expandInline(value, environ)
}

// expandInline replaces every ${NAME} in value. An unterminated directive is
// literal text, not an error: a value such as "cost is ${5" is a value, not a
// reference.
func expandInline(value string, environ []string) (string, error) {
	var out strings.Builder
	rest := value
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			out.WriteString(rest)
			return out.String(), nil
		}
		out.WriteString(rest[:start])

		end := strings.Index(rest[start:], "}")
		if end < 0 {
			out.WriteString(rest[start:])
			return out.String(), nil
		}

		name := rest[start+2 : start+end]
		resolved, found := lookup(environ, name)
		if !found {
			return "", fmt.Errorf("%w: %s", ErrUnresolvedVar, name)
		}
		out.WriteString(resolved)
		rest = rest[start+end+1:]
	}
}
