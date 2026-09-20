package webauthn

import (
	"context"
	"time"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// UserRPCPort adapts the passkey stores onto the neutral credential
// port the user package's Connect surface consumes; the direction
// keeps the user package free of a webauthn import cycle.
type UserRPCPort struct {
	service *Service
}

// NewUserRPCPort builds the port on the passkey service.
func NewUserRPCPort(service *Service) UserRPCPort {
	return UserRPCPort{service: service}
}

// CredentialsForUser lists the user's registered credentials.
func (p UserRPCPort) CredentialsForUser(ctx context.Context, userID user.UserID) ([]user.CredentialView, error) {
	stored, err := p.service.ListCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]user.CredentialView, 0, len(stored))
	for _, c := range stored {
		out = append(out, credentialBinding(c))
	}
	return out, nil
}

// RenameCredential updates one credential's display name; unknown
// IDs surface the store's not-found error.
func (p UserRPCPort) RenameCredential(ctx context.Context, userID user.UserID, credentialID, name string) (*user.CredentialView, error) {
	id, err := parseTypeID(credentialID)
	if err != nil {
		return nil, err
	}
	stored, err := p.service.RenameCredential(ctx, userID, id, name)
	if err != nil {
		return nil, err
	}
	view := credentialBinding(*stored)
	return &view, nil
}

// DeleteCredential removes one credential; unknown IDs surface the
// not-found error.
func (p UserRPCPort) DeleteCredential(ctx context.Context, userID user.UserID, credentialID string) error {
	id, err := parseTypeID(credentialID)
	if err != nil {
		return err
	}
	return p.service.DeleteCredential(ctx, userID, id)
}

// parseCredentialID accepts the TypeID form the wire carries.
func parseTypeID(raw string) (CredentialID, error) {
	return identity.ParseID[CredentialID](raw)
}

// credentialBinding maps the stored row onto the neutral projection.
func credentialBinding(c StoredCredential) user.CredentialView {
	view := user.CredentialView{
		ID:              c.ID.String(),
		Name:            c.Name,
		CredentialID:    base64URL(c.CredentialID),
		AttestationType: c.AttestationType,
		Transport:       c.Transport,
		BackupEligible:  c.BackupEligible,
		BackupState:     c.BackupState,
		CreatedAt:       c.CreatedAt.UTC().Format(time.RFC3339),
	}
	if c.LastUsedAt != nil {
		at := c.LastUsedAt.UTC().Format(time.RFC3339)
		view.LastUsedAt = &at
	}
	return view
}
