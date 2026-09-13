package webauthn

import (
	"encoding/base64"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"

	"github.com/riipandi/tango/modules/identity/user"
)

// User adapts the identity user to the go-webauthn User contract.
// The WebAuthn user handle is the user UUID bytes (upstream
// parity — discoverable login resolves users by handle).
type User struct {
	u             user.User
	credentialSet []gowebauthn.Credential
}

// NewUser wraps an identity user with its stored credentials.
func NewUser(u user.User, credentialSet []gowebauthn.Credential) *User {
	return &User{u: u, credentialSet: credentialSet}
}

// WebAuthnID returns the user handle: the UUID bytes of the ID.
func (u *User) WebAuthnID() []byte {
	return u.u.ID.UUIDBytes()
}

// WebAuthnName returns the username.
func (u *User) WebAuthnName() string { return u.u.Username }

// WebAuthnDisplayName returns the display name.
func (u *User) WebAuthnDisplayName() string { return u.u.DisplayName }

// WebAuthnCredentials returns the stored credentials.
func (u *User) WebAuthnCredentials() []gowebauthn.Credential {
	return u.credentialSet
}

// toLibraryCredential maps a stored row to the library credential.
// SignCount stays zero (the schema carries no counter column —
// upstream parity); clone detection is out of scope.
func toLibraryCredential(row StoredCredential) gowebauthn.Credential {
	return gowebauthn.Credential{
		ID:              row.CredentialID,
		PublicKey:       row.PublicKey,
		AttestationType: row.AttestationType,
		Transport:       transportTypes(row.Transport),
		Flags: gowebauthn.CredentialFlags{
			BackupEligible: row.BackupEligible,
			BackupState:    row.BackupState,
		},
		Authenticator: gowebauthn.Authenticator{AAGUID: aaguidBytes(row.AAGUID)},
	}
}

// descriptor converts a stored credential to its client-side
// exclusion descriptor.
func descriptor(row StoredCredential) protocol.CredentialDescriptor {
	return protocol.CredentialDescriptor{
		Type:         "public-key",
		CredentialID: *(*protocol.URLEncodedBase64)(&row.CredentialID),
		Transport:    transportTypes(row.Transport),
	}
}

// transportTypes maps stored transport strings to protocol types.
func transportTypes(stored []string) []protocol.AuthenticatorTransport {
	out := make([]protocol.AuthenticatorTransport, 0, len(stored))
	for _, name := range stored {
		out = append(out, protocol.AuthenticatorTransport(name))
	}
	return out
}

// aaguidBytes converts the stored AAGUID string to its 16 raw bytes.
func aaguidBytes(aaguid *string) []byte {
	if aaguid == nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*aaguid)
	if err != nil || len(raw) != 16 {
		return nil
	}
	return raw
}
