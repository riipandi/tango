package password

// TODO: HTTP endpoints for password authentication.
//
//	POST /api/password/authenticate   — sign-in with email + password
//	PUT  /api/users/{id}/password     — self or admin password change
//	POST /api/password/forgot         — request reset link (rate-limited)
//	POST /api/password/reset          — complete reset with token
//
// Mounted on the shared /api group by the identity module.
