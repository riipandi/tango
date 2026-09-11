package config

import "reflect"

// validKeys is the complete set of dotted config keys, derived from
// the Config struct's koanf tags. It is the single source of truth
// for unknown-key detection and documentation sync; adding a field
// to Config automatically adds its key here.
var validKeys = buildKeySet(reflect.TypeFor[Config](), "")

// buildKeySet walks a struct type and collects the dotted key of
// every koanf-tagged leaf field. Nested structs recurse with their
// tag as the path prefix.
func buildKeySet(t reflect.Type, prefix string) map[string]bool {
	keys := make(map[string]bool)
	for i := range t.NumField() {
		field := t.Field(i)

		name := field.Tag.Get("koanf")
		if name == "" || name == "-" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		if field.Type.Kind() == reflect.Struct {
			for key, ok := range buildKeySet(field.Type, path) {
				keys[key] = ok
			}
			continue
		}
		keys[path] = true
	}
	return keys
}
