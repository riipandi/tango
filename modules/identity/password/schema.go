// Package password owns credential hashing and verification: one
// argon2id/scrypt hash per user (PHC format via pkg/crypto), plus
// the identity lookup used by the sign-in flow. HTTP sign-in routes
// belong to the session feature.
package password

// userPasswordsTable is the table backing credential hashes. Its
// primary key is the owning user_id, so there is no separate ID type.
const userPasswordsTable = "public.user_passwords"
