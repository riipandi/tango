package user

import "uuid"

// UserTable is the users table. The migrations own the schema; this constant is
// how Go code names it, so a table rename touches one line.
const UserTable = "public.users"

// UserSchema is one row of UserTable. It lists only the columns the application
// writes, so a migration can add a column with a default without touching this
// struct. The db tags are the column names the query builder uses.
type UserSchema struct {
	ID          uuid.UUID `db:"id"`
	Username    string    `db:"username"`
	Email       string    `db:"email"`
	FirstName   string    `db:"first_name"`
	LastName    string    `db:"last_name"`
	DisplayName string    `db:"display_name"`
	IsAdmin     bool      `db:"is_admin"`
}
