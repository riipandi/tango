package webhook

import (
	"net/http"
	"net/http/httptest"
)

// newRecorder is a bare response recorder for the error-mapper tests.
func newRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

// newRequest is a minimal request carrying the request ID the
// responder envelope needs.
func newRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/webhooks", nil)
}
