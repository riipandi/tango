package fetcher

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
	"go.loglayer.dev/v3"
	"resty.dev/v3"
)

// Fetcher is the shared outbound HTTP client. Create one per
// application (or per integration) and reuse it — the underlying
// resty client pools connections.
type Fetcher struct {
	client *resty.Client
}

// New builds the shared client: an honest, sanitized User-Agent, a
// hard request timeout (resty defaults to no timeout), and one
// structured log entry per outbound request.
func New(opts Options) *Fetcher {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	log := opts.Logger
	if log == nil {
		log = loglayer.NewMock()
	}

	client := resty.New().
		SetTimeout(timeout).
		SetHeader("User-Agent", userAgent()).
		SetDebug(opts.Debug).
		AddResponseMiddleware(logResponse(log)).
		OnError(transportFailure(log))
	if opts.BaseURL != "" {
		client.SetBaseURL(opts.BaseURL)
	}
	applyResilience(client, opts, log)

	// Route resty's internal debug dump into the shared slog
	// instance when the wrapped transport exposes one.
	if slogLgr, ok := log.GetLoggerInstance("slog").(*slog.Logger); ok && slogLgr != nil {
		client.SetLogger(slogLoggerAdapter{lgr: slogLgr})
	}

	return &Fetcher{client: client}
}

// applyResilience layers the retry and circuit-breaker policies onto the client.
func applyResilience(c *resty.Client, opts Options, log logger.Logger) {
	retries := opts.Retries
	if retries == 0 {
		retries = DefaultRetries
	}
	if retries > 0 {
		c.SetRetryCount(retries).
			AddRetryConditions(
				resty.RetryConditionStatusTooManyRequests,
				resty.RetryConditionStatus5XX,
			).
			AddRetryHooks(retryHook(log))
	}
	if opts.RetryWaitTime > 0 {
		c.SetRetryWaitTime(opts.RetryWaitTime)
	}
	if opts.RetryMaxWaitTime > 0 {
		c.SetRetryMaxWaitTime(opts.RetryMaxWaitTime)
	}

	if opts.CircuitBreaker {
		breaker := resty.NewCircuitBreakerCount(
			DefaultBreakerThreshold,
			DefaultBreakerSuccess,
			DefaultBreakerReset,
		)
		breaker.OnTrigger(func(req *resty.Request, err error) {
			log.WithMetadata(loglayer.M{
				"method": req.Method,
				"url":    req.URL,
			}).WithError(err).Warn("circuit breaker triggered")
		})
		breaker.OnStateChange(func(oldState, newState resty.CircuitBreakerState) {
			log.Info("circuit breaker state changed", loglayer.M{
				"from": oldState,
				"to":   newState,
			})
		})
		c.SetCircuitBreaker(breaker)
	}
}

// retryHook records each retry attempt for observability.
func retryHook(log logger.Logger) func(*resty.Response, error) {
	return func(resp *resty.Response, err error) {
		entry := log.WithMetadata(loglayer.M{
			"method":  resp.Request.Method,
			"url":     resp.Request.URL,
			"attempt": resp.Request.Attempt,
		})
		if err != nil {
			entry.WithError(err).Debug("retrying outbound request")
			return
		}
		entry.Debug("retrying outbound request")
	}
}

// slogLoggerAdapter bridges resty's printf-style Logger interface to
// the shared slog instance, so resty's debug dump lands in the same
// structured sinks as everything else.
type slogLoggerAdapter struct {
	lgr *slog.Logger
}

func (a slogLoggerAdapter) Errorf(format string, v ...any) {
	a.lgr.Error(fmt.Sprintf(format, v...))
}

func (a slogLoggerAdapter) Warnf(format string, v ...any) {
	a.lgr.Warn(fmt.Sprintf(format, v...))
}

func (a slogLoggerAdapter) Debugf(format string, v ...any) {
	a.lgr.Debug(fmt.Sprintf(format, v...))
}

// GetJSON performs a GET request and decodes a JSON body into out.
// Non-2xx responses are surfaced as errors; the response is still
// returned for callers that need status or body details.
func (f *Fetcher) GetJSON(ctx context.Context, url string, out any) (*resty.Response, error) {
	resp, err := f.client.R().
		SetContext(ctx).
		SetResult(out).
		Get(url)
	if err != nil {
		return resp, fmt.Errorf("get %s: %w", url, err)
	}
	if !resp.IsStatusSuccess() {
		return resp, fmt.Errorf("get %s: unexpected status %d", url, resp.StatusCode())
	}
	return resp, nil
}

// PostJSON performs a POST request with a JSON body and decodes the
// JSON response into out. Non-2xx responses are surfaced as errors.
func (f *Fetcher) PostJSON(ctx context.Context, url string, body, out any) (*resty.Response, error) {
	resp, err := f.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		SetResult(out).
		Post(url)
	if err != nil {
		return resp, fmt.Errorf("post %s: %w", url, err)
	}
	if !resp.IsStatusSuccess() {
		return resp, fmt.Errorf("post %s: unexpected status %d", url, resp.StatusCode())
	}
	return resp, nil
}

// Close releases the client's connections.
func (f *Fetcher) Close() error {
	return f.client.Close()
}

// logResponse emits one structured entry per outbound request that
// reached a response, with the level escalating on the status:
// debug on 2xx, warning on 4xx, error on 5xx.
func logResponse(log logger.Logger) resty.ResponseMiddleware {
	return func(_ *resty.Client, resp *resty.Response) error {
		entry := log.WithMetadata(loglayer.M{
			"method":      resp.Request.Method,
			"url":         resp.Request.URL,
			"status":      resp.StatusCode(),
			"duration_ms": resp.Duration().Milliseconds(),
		})

		switch {
		case resp.StatusCode() >= http.StatusInternalServerError:
			entry.Error("outbound request failed")
		case resp.StatusCode() >= http.StatusBadRequest:
			entry.Warn("outbound request rejected")
		default:
			entry.Debug("outbound request served")
		}
		return nil
	}
}

// transportFailure logs the requests that never produced a
// response — connection refused, timeouts, TLS errors — which the
// response middleware never sees.
func transportFailure(log logger.Logger) resty.ErrorHook {
	return func(req *resty.Request, err error) {
		if err == nil {
			return
		}
		log.WithMetadata(loglayer.M{
			"method": req.Method,
			"url":    req.URL,
		}).WithError(err).Error("outbound request failed")
	}
}

// userAgent composes the client's User-Agent: an honest product
// token from build metadata (internal/config/meta.go). Static
// compile-time values only — nothing user-controlled, so no
// sanitization is needed.
func userAgent() string {
	return fmt.Sprintf("%s/%s", config.AppName, config.AppVersion)
}
