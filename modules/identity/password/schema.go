package password

import "uuid"

// UserPasswordTable is the table holding one password per user. The migrations
// own the schema; this constant is how Go code names it, so a table rename
// touches one line.
const UserPasswordTable = "public.user_passwords"

// UserPasswordSchema is one row of UserPasswordTable. It lists only the columns
// the application writes, so a migration can add a column with a default
// without touching this struct. The db tags are the column names the query
// builder uses.
type UserPasswordSchema struct {
	UserID       uuid.UUID `db:"user_id"`
	PasswordHash string    `db:"password_hash"`
}
