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

// Fetcher is the shared outbound HTTP client.
type Fetcher struct {
	client *resty.Client
}

// New builds a client with timeout, logging, and retry settings.
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

	// Send resty's debug output to the shared logger when available.
	if slogLgr, ok := log.GetLoggerInstance("slog").(*slog.Logger); ok && slogLgr != nil {
		client.SetLogger(slogLoggerAdapter{lgr: slogLgr})
	}

	return &Fetcher{client: client}
}

// applyResilience adds retry and circuit-breaker policies.
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

// retryHook logs retry attempts.
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

// slogLoggerAdapter adapts resty's logger to slog.
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

// GetJSON sends a GET and decodes its JSON response into out.
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

// PostJSON sends JSON and decodes the response into out.
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

// SendRaw sends one request and returns its status and body without
// treating non-2xx responses as errors.
func (f *Fetcher) SendRaw(ctx context.Context, method, url string, headers map[string]string, body []byte) (int, []byte, error) {
	req := f.client.R().SetContext(ctx).SetBody(body)
	for name, value := range headers {
		req.SetHeader(name, value)
	}

	resp, err := req.Execute(method, url)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	return resp.StatusCode(), resp.Bytes(), nil
}

// Close releases client resources.
func (f *Fetcher) Close() error {
	return f.client.Close()
}

// logResponse logs the result of each answered request.
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

// transportFailure logs requests that receive no response.
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

// userAgent returns the product and version from build metadata.
func userAgent() string {
	return fmt.Sprintf("%s/%s", config.AppName, config.AppVersion)
}
