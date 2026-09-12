// Package user is the mandatory accounts core: user entities, their
// CRUD surface, and persistence. Every other feature hangs off user
// accounts; the composition root always passes it first to
// identity.New.
package user

import (
	"context"
	"errors"

	"github.com/riipandi/tango/modules/identity"
)

// Store abstracts user persistence so a database-backed
// implementation can replace the memory store without touching
// handlers or business rules.
type Store interface {
	List(ctx context.Context) []identity.User
	Create(ctx context.Context, name string) (identity.User, error)
	GetByID(ctx context.Context, id identity.UserID) (identity.User, bool)
}

// ErrInvalidName is returned when a user payload fails validation.
var ErrInvalidName = errors.New("name is required")
