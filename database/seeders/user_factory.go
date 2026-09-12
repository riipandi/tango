// Package seeders builds deterministic development fixtures on
// top of the identity user service: fixed names plus a sequential
// suffix. Tests pin Seed so factories produce the same users on
// every run; local runs default to a crypto-randomized offset.
package seeders

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// suffixMod bounds the numeric suffix (name-xxxx).
const suffixMod = 10000

// UserFactory creates users through the user service, so fixtures
// always pass the same validation and audit rules as production
// writes. Names are base plus a sequential suffix starting from
// Seed: deterministic without any PRNG. Only the default zero
// seed draws its start from crypto/rand. Safe for concurrent use.
type UserFactory struct {
	service *user.Service

	mu   sync.Mutex
	next int
}

// NewUserFactory builds a factory on top of svc. An explicit seed
// sets the starting suffix (mod 10000); zero selects a
// crypto-randomized start for ad-hoc runs.
func NewUserFactory(svc *user.Service, seed uint64) *UserFactory {
	if seed == 0 {
		if n, err := rand.Int(rand.Reader, big.NewInt(suffixMod)); err == nil {
			seed = n.Uint64()
		}
	}
	return &UserFactory{service: svc, next: int(seed % suffixMod)}
}

// Create builds one user with the next sequential suffix,
// wrapping past 9999 back to 0000.
func (f *UserFactory) Create(ctx context.Context, base string) (identity.User, error) {
	if base == "" {
		base = "user"
	}
	f.mu.Lock()
	n := f.next
	f.next = (f.next + 1) % suffixMod
	f.mu.Unlock()

	name := fmt.Sprintf("%s_%04d", sanitizeBase(base), n)
	created, err := f.service.Create(ctx, user.CreateParams{
		Username: name,
		Email:    name + "@users.local",
	})
	if err != nil {
		return identity.User{}, fmt.Errorf("seed user %q: %w", name, err)
	}
	return created, nil
}

// sanitizeBase keeps only username-safe characters; the factory
// must never fail on fixture names.
func sanitizeBase(base string) string {
	out := make([]rune, 0, len(base))
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "user"
	}
	return string(out)
}

// CreateMany builds count users sharing the same base name,
// returning them in creation order.
func (f *UserFactory) CreateMany(ctx context.Context, base string, count int) ([]identity.User, error) {
	users := make([]identity.User, 0, count)
	for range count {
		created, err := f.Create(ctx, base)
		if err != nil {
			return nil, err
		}
		users = append(users, created)
	}
	return users, nil
}
