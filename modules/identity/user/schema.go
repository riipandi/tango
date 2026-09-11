// Package user is the core accounts subdomain of the identity module:
// user entities and their CRUD surface. It is the mandatory core —
// every other authn feature (session, webauthn, password, ...) hangs
// off user accounts — so it is not selectable; the composition root
// always passes it as the first argument to identity.New.
//
// The User type itself lives in the identity root (shared vocabulary
// for all features and for the oidc surface); this package owns
// persistence and business rules for it.
package user

import (
	"context"
	"errors"

	"github.com/riipandi/tango/modules/identity"
)

// Store abstracts user persistence. Swap in a database-backed
// implementation without touching handlers or business rules.
type Store interface {
	List(ctx context.Context) []identity.User
	Create(ctx context.Context, name string) (identity.User, error)
	GetByID(ctx context.Context, id string) (identity.User, bool)
}

// ErrInvalidName is returned when a user payload fails validation.
var ErrInvalidName = errors.New("name is required")
