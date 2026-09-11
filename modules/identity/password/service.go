package password

// TODO: business rules for password authentication.
//
//	hash      — argon2id (golang.org/x/crypto/argon2), constant-time verify
//	verify    — email + password check, audit event on success/failure
//	change    — requires current password (self) or admin privilege
//	reset     — single-use reset token, expiry, invalidate old sessions
//	policy    — min length + strength rules from appconfig
