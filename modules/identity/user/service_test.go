package user

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/modules/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"uuid"
)

// uniqueStamp yields a per-call unique suffix: the test container
// may be shared across runs and packages.
func uniqueStamp() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

func TestServiceCreateAndGet(t *testing.T) {
	store := newTestStore(t)
	var recorded []identity.AuditEvent
	svc := NewService(store, func(_ context.Context, e identity.AuditEvent) { recorded = append(recorded, e) })

	stamp := uniqueStamp()
	user, err := svc.Create(t.Context(), CreateParams{
		Username:  "john_" + stamp,
		Email:     "john-" + stamp + "@example.com",
		FirstName: "John",
		LastName:  "Doe",
	})
	require.NoError(t, err)
	assert.Equal(t, "user", user.ID.Prefix())

	// The suffix decodes to a UUIDv7 (RFC 9562): version nibble is 7.
	parsed, parseErr := uuid.Parse(user.ID.UUID())
	require.NoError(t, parseErr)
	assert.Equal(t, byte(7), parsed[6]>>4)
	assert.True(t, strings.HasPrefix(user.Username, "john_"))
	assert.Equal(t, "John Doe", user.DisplayName)

	require.Len(t, recorded, 1)
	assert.Equal(t, "user.created", recorded[0].Action)

	got, err := svc.GetByID(t.Context(), user.ID)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got.Username, "john_"))
	assert.Equal(t, user.Email, got.Email)

	missing := identity.NewID[identity.UserID]()
	_, err = svc.GetByID(t.Context(), missing)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestServiceCreateValidation(t *testing.T) {
	svc := NewService(newTestStore(t), nil)

	cases := []struct {
		name    string
		params  CreateParams
		wantErr error
	}{
		{"short username", CreateParams{Username: "ab", Email: "a@b.co"}, ErrInvalidUsername},
		{"bad username chars", CreateParams{Username: "has space", Email: "a@b.co"}, ErrInvalidUsername},
		{"missing email", CreateParams{Username: "john"}, ErrInvalidEmail},
		{"bad email", CreateParams{Username: "john", Email: "not-an-email"}, ErrInvalidEmail},
	}

	for _, tc := range cases {
		_, err := svc.Create(t.Context(), tc.params)
		assert.ErrorIs(t, err, tc.wantErr, tc.name)
	}
}

func TestServiceCreateDuplicate(t *testing.T) {
	svc := NewService(newTestStore(t), nil)
	stamp := uniqueStamp()

	_, err := svc.Create(t.Context(), CreateParams{
		Username: "john_" + stamp,
		Email:    "john-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	_, err = svc.Create(t.Context(), CreateParams{
		Username: "john_" + stamp,
		Email:    "other-" + stamp + "@example.com",
	})
	assert.ErrorIs(t, err, ErrDuplicate)
}

func TestDisplayNameFallback(t *testing.T) {
	svc := NewService(newTestStore(t), nil)

	// Unique emails per case: the container outlives a single test.
	stamp := uniqueStamp()
	cases := []struct {
		name   string
		params CreateParams
		want   string
	}{
		{"explicit", CreateParams{Username: "ada_a_" + stamp, Email: "ada-a-" + stamp + "@example.com", DisplayName: "Ada L"}, "Ada L"},
		{"first last", CreateParams{Username: "ada_b_" + stamp, Email: "ada-b-" + stamp + "@example.com", FirstName: "Ada", LastName: "L"}, "Ada L"},
		{"email local", CreateParams{Username: "ada_c_" + stamp, Email: "ada3-" + stamp + "@example.com"}, "ada3-" + stamp},
	}

	for _, tc := range cases {
		user, err := svc.Create(t.Context(), tc.params)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, user.DisplayName, tc.name)
	}
}

func TestStoreListNewestFirst(t *testing.T) {
	store := newTestStore(t)
	stamp := uniqueStamp()

	first, err := store.Create(t.Context(), CreateParams{
		Username: "list1_" + stamp, Email: "list1-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	second, err := store.Create(t.Context(), CreateParams{
		Username: "list2_" + stamp, Email: "list2-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	users := store.List(t.Context())
	require.GreaterOrEqual(t, len(users), 2)
	// The two just-created users are the newest; newest first.
	assert.Equal(t, second.ID, users[0].ID)
	assert.Equal(t, first.ID, users[1].ID)
}
