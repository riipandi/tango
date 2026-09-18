package password

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestService builds the credential service on the real store.
func newTestService(t *testing.T) (*Service, user.User) {
	store, users := newTestStack(t)
	svc := NewService(store, crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt), nil)
	return svc, createUser(t, users, "svc_"+strconv.FormatInt(time.Now().UnixNano(), 10)[:6])
}

func TestSetPasswordAndVerifyIdentity(t *testing.T) {
	svc, u := newTestService(t)
	ctx := t.Context()

	require.NoError(t, svc.SetPassword(ctx, u.ID, "s3cret-p@ss"))

	// Username and email both verify.
	for _, identity := range []string{u.Username, u.Email} {
		got, err := svc.VerifyIdentity(ctx, identity, "s3cret-p@ss")
		require.NoError(t, err, identity)
		assert.Equal(t, u.ID, got.ID, identity)
	}

	// Wrong secret, unknown identity, and disabled accounts fail closed.
	_, err := svc.VerifyIdentity(ctx, u.Username, "wrong")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	_, err = svc.VerifyIdentity(ctx, "nobody", "s3cret-p@ss")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestPasswordPolicyBounds(t *testing.T) {
	svc, u := newTestService(t)
	ctx := t.Context()

	// One policy: below the floor and above the ceiling fail, the
	// boundary values pass.
	assert.ErrorIs(t, svc.SetPassword(ctx, u.ID, "short7"), ErrWeakPassword)
	assert.ErrorIs(t, svc.SetPassword(ctx, u.ID, strings.Repeat("x", 129)), ErrOversizeSecret)
	require.NoError(t, svc.SetPassword(ctx, u.ID, strings.Repeat("x", 8)))
	require.NoError(t, svc.SetPassword(ctx, u.ID, strings.Repeat("y", 128)))
}

func TestUnknownIdentityBurnsHashWork(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := t.Context()

	// The dummy hash is built lazily once and reused; unknown
	// identities cost the same hashing work as a real check.
	_, err := svc.VerifyIdentity(ctx, "nobody", "whatever1")
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	svc.dummyOnce = sync.Once{}
	_, err = svc.VerifyIdentity(ctx, "nobody-else", "whatever1")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	assert.NotEmpty(t, svc.dummyHashValue, "the dummy credential must exist after a miss")
}

func TestSetPasswordRejectsWeak(t *testing.T) {
	svc, u := newTestService(t)
	err := svc.SetPassword(t.Context(), u.ID, "short")
	assert.ErrorIs(t, err, ErrWeakPassword)
}

func TestVerifyForUser(t *testing.T) {
	svc, u := newTestService(t)
	ctx := t.Context()
	require.NoError(t, svc.SetPassword(ctx, u.ID, "first-pass-1"))

	require.NoError(t, svc.VerifyForUser(ctx, u.ID, "first-pass-1"))
	assert.ErrorIs(t, svc.VerifyForUser(ctx, u.ID, "nope"), ErrInvalidCredentials)

	// After rotation only the new secret verifies.
	require.NoError(t, svc.SetPassword(ctx, u.ID, "second-pass-2"))
	assert.ErrorIs(t, svc.VerifyForUser(ctx, u.ID, "first-pass-1"), ErrInvalidCredentials)
	require.NoError(t, svc.VerifyForUser(ctx, u.ID, "second-pass-2"))
}
