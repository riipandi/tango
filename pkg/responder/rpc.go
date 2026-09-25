package responder

import (
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
)

// RPCStatus builds the outcome block the RPC responses carry: the word the
// client checks and the sentence a UI shows as-is. It mirrors the keys the
// REST envelope emits, so both transports read the same shape. A failed call
// never reaches a response body — the connect error carries its own code and
// message — so the block built here always reads success.
func RPCStatus(message string) *commonv1.Status {
	return &commonv1.Status{Status: StatusSuccess, Message: message}
}
