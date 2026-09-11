// Package webauthn is the passkey authentication subdomain of
// identity: WebAuthn credentials and the begin/finish ceremony
// endpoints for login and re-authentication. This is the primary
// authentication method — sign-in is passkeys-only.
//
// Planned files:
//
//	schema.go   — Passkey, WebAuthnSession contracts + credential store
//	handler.go  — /api/webauthn/* (register, login, reauthenticate begin/finish)
//	service.go  — ceremony orchestration over go-webauthn
//	store.go    — credential/session persistence (file-per-backend)
package webauthn
