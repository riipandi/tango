package transport

// field_naming_test.go proves the snake_case wire contract for every
// message the RPC surface can return, without depending on any request
// being accepted. It walks the registered proto descriptors instead of
// sampling responses, so a field added later is covered automatically.

import (
	"strings"
	"testing"

	_ "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1"
	_ "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	_ "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1"
	_ "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	_ "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	_ "github.com/riipandi/tango/codegen/proto/go/tango/webhook/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// isSnakeCase reports whether a proto field name carries no uppercase
// letter, i.e. the name protojson emits under UseProtoNames.
func isSnakeCase(name string) bool {
	return name == strings.ToLower(name)
}

// TestProtoFieldNamesAreSnakeCase guards the invariant the snake_case
// codec relies on: serializing under proto field names produces
// snake_case only while every declared field name is snake_case. A
// camelCase field name in a .proto would silently reintroduce the
// mixed naming this contract removes.
func TestProtoFieldNamesAreSnakeCase(t *testing.T) {
	var checked, offenders int

	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(file.Package()), "tango.") {
			return true
		}
		messages := file.Messages()
		for i := range messages.Len() {
			walkMessage(messages.Get(i), &checked, &offenders, t)
		}
		return true
	})

	require.Positive(t, checked, "the walk must reach the generated descriptors")
	t.Logf("checked %d proto fields across the tango packages", checked)
	assert.Zero(t, offenders, "every proto field name must be snake_case")
}

// walkMessage checks one message's fields, then its nested messages.
func walkMessage(msg protoreflect.MessageDescriptor, checked, offenders *int, t *testing.T) {
	fields := msg.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		*checked++
		if !isSnakeCase(string(field.Name())) {
			*offenders++
			t.Errorf("%s.%s: field name is not snake_case", msg.FullName(), field.Name())
		}
	}

	nested := msg.Messages()
	for i := range nested.Len() {
		walkMessage(nested.Get(i), checked, offenders, t)
	}
}

// TestProtoJSONNameDiffersFromFieldName confirms the codec override is
// load-bearing: at least one field's default JSON name is camelCase, so
// the default protojson options would break the contract. If this ever
// fails the whole set of names is already snake_case and the codec
// would be redundant.
func TestProtoJSONNameDiffersFromFieldName(t *testing.T) {
	var camelNamed []string

	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(file.Package()), "tango.") {
			return true
		}
		messages := file.Messages()
		for i := range messages.Len() {
			collectCamelJSONNames(messages.Get(i), &camelNamed)
		}
		return true
	})

	require.NotEmpty(t, camelNamed,
		"protojson's default naming must differ from the proto names, or the codec is redundant")
	t.Logf("protojson would rename %d fields, e.g. %s -> %s",
		len(camelNamed), camelNamed[0], jsonCamel(camelNamed[0]))
}

func collectCamelJSONNames(msg protoreflect.MessageDescriptor, out *[]string) {
	fields := msg.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if string(field.JSONName()) != string(field.Name()) {
			*out = append(*out, string(field.Name()))
		}
	}
	nested := msg.Messages()
	for i := range nested.Len() {
		collectCamelJSONNames(nested.Get(i), out)
	}
}

// jsonCamel renders the lowerCamelCase form protojson uses by default,
// for the log line only.
func jsonCamel(name string) string {
	parts := strings.Split(name, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}
