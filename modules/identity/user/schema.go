package user

import (
	"time"

	"uuid"
)

// UserTable is the users table. The migrations own the schema; this constant is
// how Go code names it, so a table rename touches one line.
const UserTable = "public.users"

// UserSchema is one row of UserTable. It lists only the columns the application
// writes and the account procedures read, so a migration can add a column with
// a default without touching this struct. The db tags are the column names the
// query builder uses.
type UserSchema struct {
	ID              uuid.UUID  `db:"id"`
	Username        string     `db:"username"`
	Email           string     `db:"email"`
	FirstName       string     `db:"first_name"`
	LastName        string     `db:"last_name"`
	DisplayName     string     `db:"display_name"`
	Locale          string     `db:"locale"`
	IsAdmin         bool       `db:"is_admin"`
	Disabled        bool       `db:"disabled"`
	EmailVerifiedAt *time.Time `db:"email_verified_at"`
	CreatedAt       time.Time  `db:"created_at"`
	BannedAt        *time.Time `db:"banned_at"`
	BanExpires      *time.Time `db:"ban_expires"`
	BanReason       *string    `db:"ban_reason"`

	// ProfilePicturePath is the storage key the account's picture lives
	// under; nil means the bundled default picture answers for it.
	ProfilePicturePath *string `db:"profile_picture_path"`
}
