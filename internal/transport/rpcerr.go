package transport

import (
	"connectrpc.com/connect"
)

// rpcerr centralizes the Connect error mapping for first-party RPC
// handlers. Every RPC failure surfaces as a typed Connect code with
// the message wording the REST envelope would carry; handlers must
// not invent ad-hoc codes. Field-level validation errors travel in
// the error message until a details message type is frozen in a
// later phase.
//
// REST equivalents for the cutover: 400/422 → invalid_argument,
// 401 → unauthenticated, 403 → permission_denied, 404 → not_found,
// 409 → already_exists, 429 → resource_exhausted, 5xx → internal.

// RPCInvalidArgument reports rejected input (REST 400/422).
func RPCInvalidArgument(message string) error {
	return connect.NewError(connect.CodeInvalidArgument, errString(message))
}

// RPCUnauthenticated reports missing or invalid credentials (REST 401).
func RPCUnauthenticated(message string) error {
	return connect.NewError(connect.CodeUnauthenticated, errString(message))
}

// RPCPermissionDenied reports an authenticated caller without access
// (REST 403).
func RPCPermissionDenied(message string) error {
	return connect.NewError(connect.CodePermissionDenied, errString(message))
}

// RPCNotFound reports a missing resource (REST 404).
func RPCNotFound(message string) error {
	return connect.NewError(connect.CodeNotFound, errString(message))
}

// RPCAlreadyExists reports a conflicting write (REST 409).
func RPCAlreadyExists(message string) error {
	return connect.NewError(connect.CodeAlreadyExists, errString(message))
}

// RPCResourceExhausted reports a rejected budget (rate limit, quota;
// REST 429).
func RPCResourceExhausted(message string) error {
	return connect.NewError(connect.CodeResourceExhausted, errString(message))
}

// RPCInternal reports an unexpected server failure (REST 500).
func RPCInternal(message string) error {
	return connect.NewError(connect.CodeInternal, errString(message))
}

type errString string

func (e errString) Error() string { return string(e) }
