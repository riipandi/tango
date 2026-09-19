package rpcerr

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"encoding/json/jsontext"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// codecNameJSON is the name the built-in protojson codec registers
// under. Registering another codec with the same name replaces it for
// every procedure on the handler, which is what makes the override
// effective on the Connect JSON path.
const codecNameJSON = "json"

// Options returns the handler options every first-party RPC service
// registers with: the snake_case JSON codec and panic recovery.
// Services append their own guards and interceptors after these.
func Options() []connect.HandlerOption {
	return []connect.HandlerOption{connect.WithCodec(snakeCaseJSONCodec{}), RecoverOption()}
}

// snakeCaseJSONCodec serializes proto messages under their declared
// field names (snake_case) instead of protojson's default
// lowerCamelCase. Unmarshalling stays tolerant: protojson accepts
// either spelling, so a request may use both.
type snakeCaseJSONCodec struct{}

// The codec replaces the built-in one by name, so it also implements
// the stable-marshal extension: dropping it would degrade a GET-bound
// client to a runtime "codec doesn't support stable marshal" error.
var (
	_ connect.Codec = snakeCaseJSONCodec{}
	_ stableCodec   = snakeCaseJSONCodec{}
)

type stableCodec interface {
	connect.Codec
	MarshalStable(any) ([]byte, error)
}

func (snakeCaseJSONCodec) Name() string { return codecNameJSON }

func (snakeCaseJSONCodec) Marshal(message any) ([]byte, error) {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return nil, errNotProto(message)
	}
	return protojson.MarshalOptions{UseProtoNames: true}.Marshal(protoMessage)
}

// MarshalStable backs the Connect GET binding, which needs a
// whitespace-free encoding to build a query string. protojson emits
// compact output already; compacting guards that assumption.
func (snakeCaseJSONCodec) MarshalStable(message any) ([]byte, error) {
	data, err := snakeCaseJSONCodec{}.Marshal(message)
	if err != nil {
		return nil, err
	}
	value := jsontext.Value(data)
	if err := value.Compact(); err != nil {
		return nil, fmt.Errorf("compact stable JSON: %w", err)
	}
	return value, nil
}

func (snakeCaseJSONCodec) Unmarshal(data []byte, message any) error {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return errNotProto(message)
	}
	if len(data) == 0 {
		return errors.New("zero-length payload is not a valid JSON object")
	}
	// Unknown fields are discarded so a client is not pinned to the
	// server's exact schema version; the built-in codec behaves the
	// same way.
	options := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := options.Unmarshal(data, protoMessage); err != nil {
		return fmt.Errorf("unmarshal into %T: %w", message, err)
	}
	return nil
}

func (snakeCaseJSONCodec) IsBinary() bool { return false }

func errNotProto(message any) error {
	return fmt.Errorf("%T doesn't implement proto.Message", message)
}
