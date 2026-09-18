package user

import (
	"time"

	identityv1 "github.com/riipandi/tango/gen/proto/go/tango/identity/v1"
)

// ProtoView maps the account projection onto the shared RPC message.
// Optional fields stay unset when nil; timestamps render RFC 3339
// UTC.
func ProtoView(u User) *identityv1.User {
	out := &identityv1.User{
		Id:          u.ID.String(),
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		IsAdmin:     u.IsAdmin,
		Disabled:    u.Disabled,
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
	}
	if u.FirstName != nil {
		out.FirstName = u.FirstName
	}
	if u.LastName != nil {
		out.LastName = u.LastName
	}
	if u.AvatarURL != nil {
		out.AvatarUrl = u.AvatarURL
	}
	if u.Locale != nil {
		out.Locale = u.Locale
	}
	if u.EmailVerifiedAt != nil {
		at := u.EmailVerifiedAt.UTC().Format(time.RFC3339)
		out.EmailVerifiedAt = &at
	}
	if u.UpdatedAt != nil {
		at := u.UpdatedAt.UTC().Format(time.RFC3339)
		out.UpdatedAt = &at
	}
	if u.LastLoginAt != nil {
		at := u.LastLoginAt.UTC().Format(time.RFC3339)
		out.LastLoginAt = &at
	}
	return out
}
