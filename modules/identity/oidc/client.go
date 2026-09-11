package oidc

// TODO: relying-party client management.
//
//	CRUD for confidential/public clients with callback URL
//	wildcards; client secret hashing; CIMD (Client-ID Metadata
//	Documents) resolution with allowlist; federated client
//	authentication (mTLS / private_key_jwt).
//
// Endpoints live in handler.go (/api/oidc/clients), persistence in
// the store files.
