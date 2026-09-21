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

func TestMapKeysMatchTheStruct(t *testing.T) {
	// The list is what makes a JSON object under otel.headers one value rather
	// than a section walked into dotted keys, so a new map field must be listed
	// or its entries would be dropped by filterKnown and read as unset.
	var found []string
	for _, key := range Keys() {
		if reflect.TypeOf(DefaultsMap()[key]).Kind() == reflect.Map {
			found = append(found, key)
		}
	}

	assert.Equal(t, mapKeys, found,
		"mapKeys must list exactly the map fields on Config")
}

func TestSplitMapReadsTheSpecificationForm(t *testing.T) {
	// The form the OpenTelemetry specification uses for its own headers
	// variable, which is what a directive resolves to.
	assert.Equal(t,
		map[string]string{"authorization": "Bearer x", "x-tenant": "acme"},
		splitMap("authorization=Bearer x,x-tenant=acme"))
	assert.Equal(t,
		map[string]string{"authorization": "Bearer x"},
		splitMap(" authorization = Bearer x "),
		"space around a name or a value is dropped")
	assert.Equal(t,
		map[string]string{"x-empty": ""},
		splitMap("x-empty="),
		"an empty value is a header with no value, which is legal")
	assert.Empty(t, splitMap(""), "an empty value carries no header")
	assert.Empty(t, splitMap("authorization"),
		"an entry with no = names no value and is dropped rather than sent")
	assert.Equal(t,
		map[string]string{"x-tenant": "acme"},
		splitMap("=,x-tenant=acme"),
		"a nameless entry is dropped rather than becoming an empty header name")
}

func TestNormalizeMapsLeavesAJSONObjectAlone(t *testing.T) {
	// A config file writes the map as an object, which is already the shape the
	// field wants; only a string needs splitting.
	keys := map[string]any{"otel.headers": map[string]any{"authorization": "Bearer x"}}

	normalizeMaps(keys)

	assert.Equal(t, map[string]any{"authorization": "Bearer x"}, keys["otel.headers"])
}

func TestNormalizeMapsTouchesNoOtherKey(t *testing.T) {
	// A string with an = under a key that is not a map is an ordinary value, and
	// splitting it would silently change it.
	keys := map[string]any{"server.base_url": "https://example.com?a=b"}

	normalizeMaps(keys)

	assert.Equal(t, "https://example.com?a=b", keys["server.base_url"])
}
