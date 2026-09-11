package fetcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/riipandi/tango/internal/config"
	"resty.dev/v3"
)

// Fetcher is the shared outbound HTTP client. Create one per
// application (or per integration) and reuse it — the underlying
// resty client pools connections.
type Fetcher struct {
	client *resty.Client
}

// New builds the shared client: an honest, sanitized User-Agent and
// a hard request timeout (resty defaults to no timeout).
func New(opts Options) *Fetcher {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	client := resty.New().
		SetTimeout(timeout).
		SetHeader("User-Agent", userAgent(opts.BaseURL))

	return &Fetcher{client: client}
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

// userAgent composes an honest, sanitized User-Agent:
// "name/version (+origin)". Control characters are stripped so a
// misconfigured origin can never inject headers.
func userAgent(baseURL string) string {
	ua := fmt.Sprintf("%s/%s", config.AppName, config.AppVersion)
	if baseURL != "" {
		ua += fmt.Sprintf(" (+%s)", strings.TrimRight(baseURL, "/"))
	}
	return sanitizeUserAgent(ua)
}

// sanitizeUserAgent strips control characters (header injection
// becomes impossible even with a hostile config value).
func sanitizeUserAgent(ua string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, ua)
}
