package userid_test

import (
	"testing"

	"uuid"

	"github.com/riipandi/tango/pkg/userid"
)

func TestRoundTrip(t *testing.T) {
	raw := uuid.MustParse("01a0dd5d-1bd7-7ad0-bc01-f6ca6841aa08")
	id, err := userid.FromUUID(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id.Prefix() != "user" {
		t.Fatalf("unexpected prefix %q", id.Prefix())
	}
	parsed, err := userid.Parse(id.String())
	if err != nil {
		t.Fatal(err)
	}
	back := userid.UUIDOf(parsed)
	if back != raw {
		t.Fatalf("round trip lost the row: %v", back)
	}
	if _, err := userid.Parse("01a0dd5d-1bd7-7ad0-bc01-f6ca6841aa08"); err == nil {
		t.Fatal("a bare UUID must not parse as the wire form")
	}
}
