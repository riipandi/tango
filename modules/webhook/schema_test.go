package webhook

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/pkg/responder"
)

func TestWebhookDeliveryTaskConfigRetainsFailures(t *testing.T) {
	cfg := WebhookDeliveryTask{}.Config()
	assert.Equal(t, WebhookQueue, cfg.Name)
	assert.Equal(t, WebhookMaxAttempts, cfg.MaxAttempts)
	assert.Equal(t, WebhookRequestTimeout*4/3, cfg.Timeout, "queue timeout must exceed the receiver deadline")

	require.NotNil(t, cfg.Retention)
	assert.True(t, cfg.Retention.OnlyFailed, "only failed deliveries are retained")
	assert.Equal(t, WebhookRetention, cfg.Retention.Duration)
	require.NotNil(t, cfg.Retention.Data)
	assert.True(t, cfg.Retention.Data.OnlyFailed)
}

func TestValidHeadersAcceptsOrdinaryHeaders(t *testing.T) {
	params := CreateParams{
		Name:     "header-check",
		Endpoint: "https://example.test/hook",
		Headers:  map[string]string{"X-Tenant": "acme", "X-Trace": "1"},
	}
	require.NoError(t, params.Validate())
}

func TestValidHeadersRejectsReservedAndOversized(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		wantErr string
	}{
		{"content-type is ours", map[string]string{"Content-Type": "text/plain"}, "reserved"},
		{"signature is ours", map[string]string{"x-signature": "spoof"}, "reserved"},
		{"delivery id is ours", map[string]string{"X-Webhook-Id": "spoof"}, "reserved"},
		{"empty header name", map[string]string{"  ": "value"}, "reserved"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validHeaders(tc.headers)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	// A header value beyond the byte budget is refused.
	huge := map[string]string{"X-Blob": string(make([]byte, maxHeadersBytes+1))}
	err := validHeaders(huge)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestValidHeadersRejectsTooManyHeaders(t *testing.T) {
	headers := map[string]string{}
	for i := range maxHeaderCount + 1 {
		headers["X-Header-"+string(rune('a'+i))] = "1"
	}
	err := validHeaders(headers)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many")
}

func TestValidHeadersTouchesTheFieldThroughValidation(t *testing.T) {
	// The rule reads the field via ValidateStruct reflection: prove the
	// reserved-header guard actually reaches it.
	params := CreateParams{
		Name:     "reflect-check",
		Endpoint: "https://example.test/hook",
		Headers:  map[string]string{"Host": "evil.test"},
	}
	err := params.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "reserved")
}

func TestValidateNilHeadersIsAllowed(t *testing.T) {
	params := CreateParams{Name: "no-headers", Endpoint: "https://example.test/hook"}
	require.NoError(t, params.Validate())
	assert.Nil(t, params.Headers)
}

func TestUpdateParamsMethodNormalization(t *testing.T) {
	method := " patch "
	params := UpdateParams{Method: &method}
	require.NoError(t, params.Validate())
	require.NotNil(t, params.Method)
	assert.Equal(t, "PATCH", *params.Method, "the method is upper-cased and trimmed in place")
}

func TestUpdateParamsEventTypesNormalized(t *testing.T) {
	params := UpdateParams{EventTypes: []string{" b ", "a", "a"}}
	require.NoError(t, params.Validate())
	assert.Equal(t, []string{"a", "b"}, params.EventTypes)
}

func TestUpdateParamsTrimsNameAndEndpoint(t *testing.T) {
	name := "  padded-name  "
	endpoint := "  https://example.test/hook  "
	params := UpdateParams{Name: &name, Endpoint: &endpoint}
	require.NoError(t, params.Validate())
	assert.Equal(t, "padded-name", *params.Name)
	assert.Equal(t, "https://example.test/hook", *params.Endpoint)
}

func TestCreateParamsNormalizesMethodAndEvents(t *testing.T) {
	params := CreateParams{
		Name:       "normalized",
		Endpoint:   "https://example.test/hook",
		Method:     " put ",
		EventTypes: []string{" b ", "a", "a", ""},
	}
	require.NoError(t, params.Validate())
	assert.Equal(t, "PUT", params.Method)
	assert.Equal(t, []string{"a", "b"}, params.EventTypes)
}

func TestValidateEndpointRejectsUnusableURLs(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"blank", "  ", "blank"},
		{"no host", "https://", "host"},
		{"unsupported scheme", "file:///etc/passwd", "http or https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorContains(t, validateEndpoint(tc.raw), tc.wantErr)
		})
	}
}

// TestWriteErrorMapsDomainErrors covers every branch of the error
// mapper: each module error must reach its documented status.
func TestWriteErrorMapsDomainErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"duplicate", ErrDuplicateName, http.StatusConflict},
		{"disabled", ErrDisabled, http.StatusConflict},
		{"too large", ErrTooLarge, http.StatusRequestEntityTooLarge},
		{"unknown", assert.AnError, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newRecorder()
			req := newRequest()

			responder.WriteError(w, req, tc.err)
			assert.Equal(t, tc.status, w.Code)
		})
	}
}

func TestWriteErrorMapsValidationErrors(t *testing.T) {
	params := CreateParams{Name: "ab", Endpoint: "https://example.test"}
	err := params.Validate()
	require.Error(t, err)

	w := newRecorder()
	responder.WriteError(w, newRequest(), err)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}
