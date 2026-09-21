package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListKeysMatchTheStruct(t *testing.T) {
	// The list is what makes a comma-separated string a list, so a new slice
	// field must be listed or a directive naming several entries would decode
	// into a one-element list holding "a,b".
	var found []string
	for _, key := range Keys() {
		if reflect.TypeOf(DefaultsMap()[key]).Kind() == reflect.Slice {
			found = append(found, key)
		}
	}

	assert.Equal(t, listKeys, found,
		"listKeys must list exactly the slice fields on Config")
}

func TestSplitListReadsACommaSeparatedValue(t *testing.T) {
	// The form an environment variable carries, which is what a directive
	// resolves to.
	assert.Equal(t, []string{"console", "file", "otlp"}, splitList("console,file,otlp"))
	assert.Equal(t, []string{"console", "file"}, splitList("console, file"),
		"space around an entry is dropped")
	assert.Equal(t, []string{"console"}, splitList("console"))
	assert.Equal(t, []string{"console"}, splitList(" console "))
	assert.Equal(t, []string{"console", "file"}, splitList("console,,file"),
		"an empty entry names no transport and is dropped")
	assert.Empty(t, splitList(""), "an empty value is an empty list, not one empty name")
	assert.Empty(t, splitList(" , "))
}

func TestNormalizeListsLeavesAJSONArrayAlone(t *testing.T) {
	// A config file writes the list as an array, which is already the shape the
	// field wants; only a string needs splitting.
	keys := map[string]any{"log.transport": []any{"console", "file"}}

	normalizeLists(keys)

	assert.Equal(t, []any{"console", "file"}, keys["log.transport"])
}

func TestNormalizeListsTouchesNoOtherKey(t *testing.T) {
	// A comma-separated string under a key that is not a list is an ordinary
	// value, and splitting it would silently change it.
	keys := map[string]any{"mailer.smtp_host": "smtp.example.com,smtp2.example.com"}

	normalizeLists(keys)

	assert.Equal(t, "smtp.example.com,smtp2.example.com", keys["mailer.smtp_host"])
}
