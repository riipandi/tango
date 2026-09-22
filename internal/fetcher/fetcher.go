// Package fetcher is the outbound HTTP client integrations call external
// services through.
//
// One client is built from the configuration and shared by the process. Resty
// issues the request; this package decides which failures are transient, which
// error class a caller can match, and which parts of a call are safe to log.
package fetcher

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"resty.dev/v3"

	"github.com/riipandi/tango/internal/config"
)

// maxResponseBytes is how much of a response body is kept. An integration
// that answers with an unbounded body must not be able to grow the process
// without limit. The remainder is refused rather than buffered.
const maxResponseBytes = 16 << 20

// Request is one outbound call. URL may be absolute, or relative to the
// configured base URL. Query and Headers are added to that request only.
// Body is encoded as JSON when it is a struct or a map; a string or a byte
// slice is sent as-is.
type Request struct {
	Method  string
	URL     string
	Query   url.Values
	Headers http.Header
	Body    any
}

// Response is the answer a call received. Body is present for an HTTP error
// status as well as a success, so a caller can read an upstream error
// document. It is never written to the log.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Attempts   int
}

// Client calls external HTTP services. It is safe for concurrent use.
// Shutdown releases its idle connections; the composition root calls it.
type Client struct {
	http       *resty.Client
	log        *slog.Logger
	baseURL    string
	retryCount int
}

// New builds the client the configuration describes.
//
// log is the process logger. Nil discards every line, which is what a test
// that is not reading the log wants. The client does not enable Resty's
// debug log: that log includes request and response bodies.
func New(cfg config.Config, log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	settings := cfg.Fetcher
	if err := ready(settings); err != nil {
		return nil, err
	}

	// The dial and the handshake share the attempt budget. A transport
	// timeout longer than the attempt would still be cut by the attempt
	// context; keeping them equal means neither one waits alone.
	httpClient := resty.NewWithTransportSettings(&resty.TransportSettings{
		DialerTimeout:         settings.Timeout,
		TLSHandshakeTimeout:   settings.Timeout,
		ResponseHeaderTimeout: settings.Timeout,
	})
	// Both thresholds are non-negative here: ready refused anything else.
	// The comparison is what makes the conversion to the breaker's uint64
	// a value that cannot wrap.
	if settings.CircuitFailureThreshold < 0 || settings.CircuitSuccessThreshold < 0 {
		return nil, fmt.Errorf("fetcher: circuit thresholds must be positive")
	}
	failures := settings.CircuitFailureThreshold
	successes := settings.CircuitSuccessThreshold
	breaker := resty.NewCircuitBreakerCount(
		uint64(failures),
		uint64(successes),
		settings.CircuitResetTimeout,
		breakerFailure,
	)
	breaker.OnStateChange(func(from, to resty.CircuitBreakerState) {
		log.Warn("fetcher: circuit breaker", "from", circuitState(from), "to", circuitState(to))
	})
	breaker.OnTrigger(func(*resty.Request, error) {
		// The request is not logged: it can carry a credential, and the
		// only fact that matters here is that the call was not sent.
		log.Warn("fetcher: circuit open")
	})

	httpClient.
		SetLogger(slogAdapter{log: log}).
		SetDebug(false).
		SetTimeout(settings.Timeout).
		SetHeader(headerUserAgent, settings.UserAgent).
		SetRetryCount(settings.RetryCount).
		SetRetryWaitTime(settings.RetryWait).
		SetRetryMaxWaitTime(settings.RetryMaxWait).
		AddRetryConditions(retryable).
		SetCircuitBreaker(breaker).
		SetCookieJar(nil).
		SetResponseBodyLimit(maxResponseBytes).
		// A redirect to another host must not carry Authorization or Cookie.
		SetRedirectPolicy(resty.RedirectHeaderStripSensitivePolicy(true))
	if settings.BaseURL != "" {
		httpClient.SetBaseURL(settings.BaseURL)
	}

	return &Client{
		http:       httpClient,
		log:        log,
		baseURL:    settings.BaseURL,
		retryCount: settings.RetryCount,
	}, nil
}

// headerUserAgent is the canonical header name, so a caller-supplied
// User-Agent replaces the product token instead of being sent beside it.
const headerUserAgent = "User-Agent"

// ready refuses a configuration that would wait without a deadline or retry
// without a bound. Validate is the message a file sees; this is the guard
// on the constructor a test or a caller can hit without it.
func ready(settings config.Fetcher) error {
	switch {
	case settings.Timeout <= 0:
		return fmt.Errorf("fetcher: timeout must be positive")
	case settings.RetryCount < 0:
		return fmt.Errorf("fetcher: retry count must not be negative")
	case settings.RetryWait <= 0 || settings.RetryMaxWait < settings.RetryWait:
		return fmt.Errorf("fetcher: retry wait must be positive and within its maximum")
	case settings.CircuitFailureThreshold <= settings.RetryCount:
		return fmt.Errorf("fetcher: circuit failure threshold must exceed the retry count")
	case settings.CircuitSuccessThreshold <= 0:
		return fmt.Errorf("fetcher: circuit success threshold must be positive")
	case settings.CircuitResetTimeout < settings.Timeout:
		return fmt.Errorf("fetcher: circuit reset timeout must not be shorter than the attempt timeout")
	case settings.UserAgent == "":
		return fmt.Errorf("fetcher: user agent must not be empty")
	default:
		return nil
	}
}

// Do sends req and returns the response it received.
//
// An HTTP status of 400 or above is an error and the response is still
// returned, so the caller can read the upstream document. A transport
// failure, a timeout, a cancellation, an open circuit, and a string of
// retries that never succeeded are different errors: errors.Is matches the
// sentinel for each. The caller's context ends the attempt and any wait
// between retries.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	method = strings.ToUpper(method)

	call := c.http.R().SetContext(ctx)
	if len(req.Query) > 0 {
		call.SetQueryParamsFromValues(req.Query)
	}
	if len(req.Headers) > 0 {
		call.SetHeaderMultiValues(req.Headers)
	}
	if req.Body != nil {
		call.SetBody(req.Body)
	}

	started := time.Now()
	res, err := call.Execute(method, req.URL)
	out, readErr := responseFrom(res)
	if readErr != nil && err == nil {
		err = readErr
	}
	classified := classify(c.retryCount, c.baseURL, method, req.URL, out.Attempts, res, err)
	c.logCall(ctx, method, safeTarget(c.baseURL, req.URL), out, classified, time.Since(started))
	if classified != nil {
		return out, classified
	}
	return out, nil
}

// Shutdown closes idle connections. In-flight calls finish on their own
// context; this does not cancel them.
func (c *Client) Shutdown(context.Context) error {
	if c == nil || c.http == nil {
		return nil
	}
	return c.http.Close()
}

// responseFrom copies the fields a caller is allowed to keep. The Resty
// response stays inside this package: its request still holds headers.
func responseFrom(res *resty.Response) (*Response, error) {
	out := &Response{Attempts: attemptsOf(res)}
	if res == nil {
		return out, nil
	}
	out.StatusCode = statusCode(res)
	if res.RawResponse != nil {
		out.Header = res.Header().Clone()
	}
	body, err := readBody(res)
	out.Body = body
	return out, err
}

// readBody returns the buffered body, or reads it once, and never more than
// maxResponseBytes. A body Resty already refused for size stays refused.
func readBody(res *resty.Response) ([]byte, error) {
	if buffered := res.Bytes(); len(buffered) > 0 {
		if len(buffered) > maxResponseBytes {
			return nil, errResponseTooLarge
		}
		return buffered, nil
	}
	if res.Body == nil {
		return nil, nil
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return body, err
	}
	if len(body) > maxResponseBytes {
		return nil, errResponseTooLarge
	}
	return body, nil
}

func attemptsOf(res *resty.Response) int {
	if res == nil || res.Request == nil || res.Request.Attempt < 1 {
		return 1
	}
	return res.Request.Attempt
}

func statusCode(res *resty.Response) int {
	if res == nil || res.RawResponse == nil {
		return 0
	}
	return res.StatusCode()
}

// logCall records the outcome of a call without the URL query, the body, or
// any header. A success is a debug line; an HTTP client error is a warning;
// everything else is an error.
func (c *Client) logCall(ctx context.Context, method, target string, res *Response, err error, elapsed time.Duration) {
	status := 0
	attempts := 1
	if res != nil {
		status = res.StatusCode
		attempts = res.Attempts
	}
	attrs := []any{
		"method", method,
		"target", target,
		"status", status,
		"attempts", attempts,
		"duration", elapsed.String(),
	}
	switch {
	case err == nil:
		c.log.DebugContext(ctx, "fetcher: call", attrs...)
	case isStatus(err) && status >= 400 && status < 500:
		c.log.WarnContext(ctx, "fetcher: call", append(attrs, "err", err.Error())...)
	default:
		c.log.ErrorContext(ctx, "fetcher: call", append(attrs, "err", err.Error())...)
	}
}
