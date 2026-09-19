package rpcerr

import (
	"encoding/json"
	"testing"

	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestSnakeCaseCodecIdentity pins the registration contract: the codec
// must claim the built-in "json" name so WithCodec replaces the default
// protojson codec, and it must expose the stable-marshal extension the
// Connect GET binding requires.
func TestSnakeCaseCodecIdentity(t *testing.T) {
	codec := snakeCaseJSONCodec{}
	assert.Equal(t, "json", codec.Name())
	assert.False(t, codec.IsBinary())

	var _ stableCodec = codec
}

// TestSnakeCaseCodecMarshalsProtoFieldNames pins the wire naming: a
// multi-word proto field reaches the wire under its declared name,
// where the built-in protojson codec would emit lowerCamelCase.
func TestSnakeCaseCodecMarshalsProtoFieldNames(t *testing.T) {
	codec := snakeCaseJSONCodec{}

	out, err := codec.Marshal(&systemv1.CurrentResponse{CurrentVersion: "0.0.0"})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))
	assert.Equal(t, "0.0.0", decoded["current_version"])
	assert.NotContains(t, decoded, "currentVersion")
}

// TestSnakeCaseCodecAcceptsBothRequestSpellings pins request
// tolerance: protojson resolves the declared name and its camelCase
// alias onto the same field.
func TestSnakeCaseCodecAcceptsBothRequestSpellings(t *testing.T) {
	codec := snakeCaseJSONCodec{}

	for _, body := range []string{
		`{"current_version":"0.0.0"}`,
		`{"currentVersion":"0.0.0"}`,
	} {
		msg := &systemv1.CurrentResponse{}
		require.NoError(t, codec.Unmarshal([]byte(body), msg), "body: %s", body)
		assert.Equal(t, "0.0.0", msg.GetCurrentVersion(), "body: %s", body)
	}
}

// TestSnakeCaseCodecStableMarshalStripsWhitespace pins the GET binding
// requirement: the stable encoding carries no whitespace, because it
// becomes a query string.
func TestSnakeCaseCodecStableMarshalStripsWhitespace(t *testing.T) {
	codec := snakeCaseJSONCodec{}

	out, err := codec.MarshalStable(&systemv1.CurrentResponse{CurrentVersion: "0.0.0"})
	require.NoError(t, err)
	assert.NotContains(t, string(out), " ")
	assert.NotContains(t, string(out), "\n")
}

// TestSnakeCaseCodecRejectsNonProto pins the type guard the built-in
// codec also applies.
func TestSnakeCaseCodecRejectsNonProto(t *testing.T) {
	codec := snakeCaseJSONCodec{}
	_, err := codec.Marshal(map[string]string{"a": "b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proto.Message")

	require.Error(t, codec.Unmarshal([]byte(`{"a":"b"}`), map[string]string{}))
	require.Error(t, codec.Unmarshal(nil, &structpb.Struct{}), "zero-length payload is rejected")
}
