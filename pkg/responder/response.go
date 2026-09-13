// Package responder writes the standard API envelope defined in
// docs/response.md: success and error responses share the same shape with
// metadata (request id, trace id, rate limit, pagination) and HATEOAS links.
package responder

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	jsonv2 "encoding/json/v2"

	"go.jetify.com/typeid"
)

// Envelope status values.
const (
	StatusSuccess = "success"
	StatusError   = "error"
)

const (
	requestIDHeader = "X-Request-Id"
	traceIDHeader   = "X-Trace-Id"
)

type requestPrefix struct{}

func (requestPrefix) Prefix() string { return "req" }

// RequestID identifies a single API request for tracing and support.
type RequestID = typeid.TypeID[requestPrefix]

// requestIDContextKey scopes the request ID inside a request context.
type requestIDContextKey struct{}

// NewRequestID generates a fresh request ID: a TypeID whose UUIDv7
// suffix is K-sortable, so log entries order by request start time.
func NewRequestID() string {
	return typeid.Must(typeid.New[RequestID]()).String()
}

// WithRequestID attaches a request ID to the context; middleware
// resolves it once and responders/loggers read it back.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, id)
}

// RequestIDFromContext returns the context request ID, empty when
// absent.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

// Envelope is the standard API response wrapper. Success responses carry
// Data, error responses carry Error; Message and Links are optional on both.
type Envelope struct {
	Status   string   `json:"status"`
	Message  string   `json:"message,omitempty"`
	Data     any      `json:"data,omitzero"`
	Error    any      `json:"error,omitzero"`
	Metadata Metadata `json:"metadata"`
	Links    Links    `json:"links,omitzero"`
}

// Metadata carries request context for both success and error envelopes.
// Pagination fields are spread directly into the object (HATEOAS-first).
type Metadata struct {
	StatusCode int        `json:"status_code"`
	RequestID  string     `json:"request_id"`
	TraceID    string     `json:"trace_id,omitempty"`
	RateLimit  *RateLimit `json:"rate_limit,omitempty"`

	Page           *int `json:"page,omitempty"`
	Limit          *int `json:"limit,omitempty"`
	TotalPages     *int `json:"total_pages,omitempty"`
	TotalItems     *int `json:"total_items,omitempty"`
	FirstItemIndex *int `json:"first_item_index,omitempty"`
	LastItemIndex  *int `json:"last_item_index,omitempty"`
}

// RateLimit reports the per-window allowance of the rate limiter.
type RateLimit struct {
	Limit     int   `json:"limit"`
	Remaining int   `json:"remaining"`
	Reset     int64 `json:"reset"` // unix timestamp when the window resets
}

// Links is a HATEOAS-friendly link map keyed by link relation ("self",
// "next", "prev", "first", "last", or any custom rel). A nil entry
// marshals as null.
type Links map[string]*string

// Option customizes the envelope built by Success and Fail.
type Option func(*Envelope)

// WithMessage sets the envelope message.
func WithMessage(message string) Option {
	return func(e *Envelope) { e.Message = message }
}

// WithError attaches structured error details (an object or array).
func WithError(detail any) Option {
	return func(e *Envelope) { e.Error = detail }
}

// WithPagination embeds precomputed pagination metadata.
func WithPagination(p Pagination) Option {
	return func(e *Envelope) { e.Metadata.applyPagination(p) }
}

// WithPaginationFrom computes pagination metadata from the request
// params and the total item count.
func WithPaginationFrom(params PaginationParams, totalItems int) Option {
	return WithPagination(NewPagination(params, totalItems))
}

// WithRateLimit embeds rate limit metadata, overriding header detection.
func WithRateLimit(rl RateLimit) Option {
	return func(e *Envelope) { e.Metadata.RateLimit = &rl }
}

// WithLinks sets the whole link map.
func WithLinks(links Links) Option {
	return func(e *Envelope) { e.Links = links }
}

// WithLink sets a single link relation.
func WithLink(rel, url string) Option {
	return func(e *Envelope) {
		if e.Links == nil {
			e.Links = Links{}
		}
		e.Links[rel] = new(url)
	}
}

// Success writes a success envelope with an optional data payload.
func Success(w http.ResponseWriter, r *http.Request, status int, data any, opts ...Option) {
	env := &Envelope{Status: StatusSuccess, Data: data, Metadata: newMetadata(w, r, status)}
	writeEnvelope(w, env, opts)
}

// Fail writes an error envelope. Error details go through WithError.
func Fail(w http.ResponseWriter, r *http.Request, status int, message string, opts ...Option) {
	env := &Envelope{Status: StatusError, Message: message, Metadata: newMetadata(w, r, status)}
	writeEnvelope(w, env, opts)
}

// WriteJSON serializes v with encoding/json/v2 (faster unmarshal
// path, stricter defaults) directly into the response writer.
// Bypasses the envelope for non-API endpoints (health probes, discovery).
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := jsonv2.MarshalWrite(w, v); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// NotFoundJSON writes a 404 error envelope.
func NotFoundJSON(w http.ResponseWriter, r *http.Request) {
	Fail(w, r, http.StatusNotFound, "not found")
}

// MethodNotAllowedJSON writes a 405 error envelope.
func MethodNotAllowedJSON(w http.ResponseWriter, r *http.Request) {
	Fail(w, r, http.StatusMethodNotAllowed, "method not allowed")
}

// BadRequestJSON writes a 400 error envelope.
func BadRequestJSON(w http.ResponseWriter, r *http.Request, message string) {
	Fail(w, r, http.StatusBadRequest, message)
}

func writeEnvelope(w http.ResponseWriter, env *Envelope, opts []Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(env)
		}
	}
	WriteJSON(w, env.Metadata.StatusCode, env)
}

func newMetadata(w http.ResponseWriter, r *http.Request, status int) Metadata {
	m := Metadata{StatusCode: status, RequestID: requestID(w, r)}
	if trace := strings.TrimSpace(r.Header.Get(traceIDHeader)); trace != "" {
		m.TraceID = trace
	}
	m.RateLimit = rateLimitFromHeaders(w.Header())
	return m
}

// requestID prefers the context value set by the request-ID
// middleware, then the incoming X-Request-Id header (echoed back as
// a response header), and generates one when both are absent.
func requestID(w http.ResponseWriter, r *http.Request) string {
	if id := RequestIDFromContext(r.Context()); id != "" {
		w.Header().Set(requestIDHeader, id)
		return id
	}

	id := strings.TrimSpace(r.Header.Get(requestIDHeader))
	if id == "" {
		id = NewRequestID()
	}
	w.Header().Set(requestIDHeader, id)
	return id
}

// rateLimitFromHeaders reads standard X-RateLimit-* headers (set by the
// rate-limit middleware earlier in the chain), nil when absent.
func rateLimitFromHeaders(h http.Header) *RateLimit {
	limit, err1 := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, err2 := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	reset, err3 := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil
	}
	return &RateLimit{Limit: limit, Remaining: remaining, Reset: reset}
}
