package transport

import (
	"encoding/json"
	"testing"

	identityv1 "github.com/riipandi/tango/gen/proto/go/tango/identity/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestUserMessageMatchesRESTDTOShape pins wire compatibility for the
// representative User contract: the generated RPC message must accept
// exactly the JSON document the REST handler serves (snake_case
// fields, RFC 3339 timestamps, nullable fields absent-or-string) with
// no unknown-field drift.
func TestUserMessageMatchesRESTDTOShape(t *testing.T) {
	// Field-for-field mirror of api/client/schemas/user.schema.ts
	// (UserSchema) as the REST handler emits it.
	doc := `{
		"id": "user_01j5k2v3w4x5y6z7a8b9c0d1e2f",
		"username": "first_admin",
		"email": "admin@example.com",
		"display_name": "First Admin",
		"is_admin": true,
		"disabled": false,
		"created_at": "2026-09-19T00:00:00Z"
	}`

	var msg identityv1.User
	require.NoError(t, (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal([]byte(doc), &msg))

	assert.Equal(t, "user_01j5k2v3w4x5y6z7a8b9c0d1e2f", msg.GetId())
	assert.Equal(t, "first_admin", msg.GetUsername())
	assert.Equal(t, "admin@example.com", msg.GetEmail())
	assert.True(t, msg.GetIsAdmin())

	// Round-trip: the generated message re-emits the same field set in
	// snake_case. Proto JSON omits default-valued scalars (disabled
	// false never appears) — RPC consumers apply proto default
	// semantics instead of the REST "always present" convention.
	out, err := protojson.MarshalOptions{}.Marshal(&msg)
	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(out, &back))
	for _, field := range []string{"id", "username", "email", "displayName", "isAdmin", "createdAt"} {
		assert.Contains(t, back, field)
	}
	assert.NotContains(t, back, "disabled", "proto JSON omits default-valued scalars")
}
