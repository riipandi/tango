// Package userid is the wire form of an account's identifier: a TypeID with
// the `user_` prefix, encoded from the row's UUID at the boundary a response
// or a token crosses and decoded back at the boundary a query crosses. The
// column stays a UUID — the point is what a reader sees, not how the row is
// keyed — so the conversion lives here and nowhere else.
package userid

import (
	"fmt"
	"uuid"

	"go.jetify.com/typeid"
)

// Prefix is the TypeID prefix of an account's identifier. An id leaves the
// server in a token subject and an API response, so the reader of a log line
// or a support ticket can tell what it names without a lookup.
type Prefix struct{}

// Prefix reports the TypeID prefix.
func (Prefix) Prefix() string { return "user" }

// ID is the typed identifier of one row of the users table, in its wire form.
type ID = typeid.TypeID[Prefix]

// FromUUID wraps the row's UUID into the wire form. It is the one direction
// every response and every signed token takes.
func FromUUID(raw uuid.UUID) (ID, error) {
	return typeid.FromUUID[ID](raw.String())
}

// FromUUIDString wraps a UUID in its text form into the wire form. Rows scan
// as text in several seams, so this is the shape those callers take.
func FromUUIDString(raw string) (ID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return ID{}, fmt.Errorf("userid: %w", err)
	}
	return FromUUID(parsed)
}

// Wire renders the wire form of a row's UUID. Rows read from the database
// always carry a valid UUID, so the render cannot fail; an invalid one
// answers the empty string, which no consumer should mistake for an id.
func Wire(raw uuid.UUID) string {
	id, err := FromUUID(raw)
	if err != nil {
		return ""
	}
	return id.String()
}

// Parse reads the wire form back. It is the boundary a request crosses: an
// identifier that arrives without the prefix names nothing this server
// speaks about, and the caller refuses it as the not-found it is.
func Parse(wire string) (ID, error) {
	parsed, err := typeid.Parse[ID](wire)
	if err != nil {
		return ID{}, fmt.Errorf("userid: %w", err)
	}
	return parsed, nil
}

// UUIDOf unwraps the wire form into the UUID the column stores. The typed id
// carries the bytes itself, so nothing re-parses text to get there.
func UUIDOf(id ID) uuid.UUID {
	return uuid.UUID(id.UUIDBytes())
}
