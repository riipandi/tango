package user

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/riipandi/tango/modules/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceCreateAndGet(t *testing.T) {
	store := NewMemoryStore()
	var recorded []identity.AuditEvent
	svc := NewService(store, func(e identity.AuditEvent) { recorded = append(recorded, e) })

	user, err := svc.Create(context.Background(), "John")
	require.NoError(t, err)
	assert.Equal(t, "user", user.ID.Prefix())

	// The suffix decodes to a UUIDv7 (RFC 9562): version nibble is 7.
	gotUUID, parseErr := uuid.Parse(user.ID.UUID())
	require.NoError(t, parseErr)
	assert.Equal(t, uuid.Version(7), gotUUID.Version())
	assert.Equal(t, "John", user.Name)

	require.Len(t, recorded, 1)
	assert.Equal(t, "user.created", recorded[0].Action)

	got, err := svc.GetByID(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, "John", got.Name)

	missing := identity.NewID[identity.UserID]()
	_, err = svc.GetByID(context.Background(), missing)
	assert.Error(t, err)
}

func TestServiceCreateValidation(t *testing.T) {
	svc := NewService(NewMemoryStore(), nil)

	_, err := svc.Create(context.Background(), "")
	require.ErrorIs(t, err, ErrInvalidName)
}

func TestMemoryStoreList(t *testing.T) {
	store := NewMemoryStore()

	_, err := store.Create(context.Background(), "A")
	require.NoError(t, err)
	_, err = store.Create(context.Background(), "B")
	require.NoError(t, err)

	users := store.List(context.Background())
	require.Len(t, users, 2)
	assert.Equal(t, "A", users[0].Name)
	assert.Equal(t, "B", users[1].Name)
}
