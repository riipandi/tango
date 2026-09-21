package config

import (
	"fmt"
	"strings"
	"time"

	"encoding/json/jsontext"
	"encoding/json/v2"
)

// secretKeys are the keys a generated file must not carry a literal value for.
// A secret is a property of the key, not of its default: server.base_url has an
// empty default too, and it is not a secret.
//
// A test asserts this list is exactly the set of keys Redacted replaces, so the
// two cannot drift: adding a secret to one without the other fails.
var secretKeys = []string{
	"app.secret_key",
	"auth.private_key",
	"auth.public_key",
	"auth.secret_key",
	"database.url",
}

// Sample renders the config file a fresh checkout starts from: every key with its
// built-in default, and every secret as an env: directive naming the variable
// key:generate writes for it.
//
// Every key is written out rather than only the ones a user is likely to change,
// so the file doubles as the list of what can be configured. The output is
// deterministic: keys are sorted, so two runs produce the same bytes.
func Sample() ([]byte, error) {
	flat := DefaultsMap()
	for _, key := range secretKeys {
		if _, ok := flat[key]; !ok {
			return nil, fmt.Errorf("config: secret key %s is not part of Config", key)
		}
		flat[key] = "env:" + EnvName(key)
	}

	out, err := json.Marshal(nest(flat), jsontext.WithIndent("    "), json.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("config: render sample: %w", err)
	}
	return append(out, '\n'), nil
}

// nest turns flat dotted keys back into the tree the file is written as. A
// duration is written as a number of seconds, which is the unit the file uses
// everywhere: "15m0s" reads as a Go expression, and 900 reads as a duration.
func nest(flat map[string]any) map[string]any {
	out := make(map[string]any)
	for key, value := range flat {
		parts := strings.Split(key, Delim)
		node := out
		for _, part := range parts[:len(parts)-1] {
			child, ok := node[part].(map[string]any)
			if !ok {
				child = make(map[string]any)
				node[part] = child
			}
			node = child
		}
		node[parts[len(parts)-1]] = renderable(value)
	}
	return out
}

// renderable converts a default into a value JSON can hold. A duration becomes a
// number of seconds, so the file says 900 rather than "15m0s". An exact division
// stays an integer: 900 seconds is written 900, not 900.0.
func renderable(value any) any {
	duration, ok := value.(time.Duration)
	if !ok {
		return value
	}
	seconds := duration.Seconds()
	if seconds == float64(int64(seconds)) {
		return int64(seconds)
	}
	return seconds
}
