package config

import "strings"

// listKeys are the config keys whose value is a list of names, and which a
// config file therefore writes as a JSON array.
//
// A directive is the reason this exists. `"transport": "env:LOG_TRANSPORT"` is
// one string by the time the file layer has resolved it, and a list written as
// one comma-separated string is the only form an environment variable can carry:
// `LOG_TRANSPORT=console,file`. Without this step koanf decodes that string into
// a one-element list holding "console,file", which names no sink.
//
// A test asserts this list is exactly the set of slice-valued fields on Config,
// so adding one fails until it is listed here.
var listKeys = []string{
	"log.transport",
}

// normalizeLists reads every list key of a layer as a list of names when it
// holds a string, so a directive can name several with commas.
//
// Only a named key is split, and only a string: a JSON array is already the
// shape the field wants, and a string under any other key is an ordinary value.
func normalizeLists(keys map[string]any) {
	for _, key := range listKeys {
		value, ok := keys[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		keys[key] = splitList(text)
	}
}

// splitList reads a comma-separated list. Space around an entry is dropped, so
// `console, file` and `console,file` mean the same thing, and an empty entry is
// dropped rather than kept as a name no transport matches.
func splitList(text string) []string {
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
