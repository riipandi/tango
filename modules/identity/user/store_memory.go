package user

import (
	"context"
	"sync"

	"uuid"

	"github.com/riipandi/tango/modules/identity"
)

// MemoryStore is an in-memory Store guarded by a mutex. A Postgres
// implementation lands later as store_postgres.go in this package.
type MemoryStore struct {
	mu    sync.Mutex
	users []identity.User
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) List(_ context.Context) []identity.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]identity.User, len(s.users))
	copy(out, s.users)
	return out
}

func (s *MemoryStore) Create(_ context.Context, name string) (identity.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// UUIDv7: time-ordered (index-friendly) per RFC 9562.
	user := identity.User{ID: uuid.NewV7().String(), Name: name}
	s.users = append(s.users, user)
	return user, nil
}

func (s *MemoryStore) GetByID(_ context.Context, id string) (identity.User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.ID == id {
			return u, true
		}
	}
	return identity.User{}, false
}
