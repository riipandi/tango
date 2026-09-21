package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

// exampleFile is the sample config a user copies to app.config.json.
const exampleFile = "app.config.json.example"

// repoRoot walks up from the test's directory to the module root, so the test
// finds the example file wherever the package is run from.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "reached the filesystem root without finding go.mod")
		dir = parent
	}
}

func TestExampleConfigListsEveryKey(t *testing.T) {
	// The example must name every key, so a user editing it can see the whole
	// configuration. A key missing here is a key nobody discovers.
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), exampleFile))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))

	assert.ElementsMatch(t, config.Keys(), flatKeys(doc, nil))
}

func TestExampleConfigIsLoadable(t *testing.T) {
	// The example must parse and be accepted, so copying it is a working start
	// rather than a file that fails on the first run.
	path := filepath.Join(repoRoot(t), exampleFile)

	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: baseEnv()})
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestExampleConfigKeepsNoSecrets(t *testing.T) {
	// The file is committed, so it must not carry a credential; an empty string
	// means "supply this", which is what a template should say.
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), exampleFile))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))

	for _, key := range []string{"database.url", "app.secret_key", "auth.private_key", "auth.secret_key"} {
		value, found := lookupPath(doc, key)
		if !found {
			continue
		}
		assert.Empty(t, value, "%s must be empty in the example", key)
	}
}

// flatKeys returns every leaf path of a nested map, the form config.Keys uses.
func flatKeys(nested map[string]any, prefix []string) []string {
	var out []string
	for key, value := range nested {
		path := append(append([]string{}, prefix...), key)
		if child, ok := value.(map[string]any); ok {
			out = append(out, flatKeys(child, path)...)
			continue
		}
		out = append(out, joinPath(path))
	}
	return out
}

// lookupPath reads a dotted path from a nested map.
func lookupPath(nested map[string]any, path string) (any, bool) {
	current := nested
	for {
		head, rest, found := cutPath(path)
		value, ok := current[head]
		if !ok {
			return nil, false
		}
		if !found {
			return value, true
		}
		current, ok = value.(map[string]any)
		if !ok {
			return nil, false
		}
		path = rest
	}
}

// cutPath splits the first segment of a dotted path from the rest.
func cutPath(path string) (string, string, bool) {
	for i := range len(path) {
		if path[i] == '.' {
			return path[:i], path[i+1:], true
		}
	}
	return path, "", false
}

// joinPath joins path segments with the config delimiter.
func joinPath(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += config.Delim
		}
		out += part
	}
	return out
}
