package webhook

import (
	"context"
	"fmt"

	"github.com/riipandi/tango/internal/fetcher"
)

// fetcherSender delivers through the shared outbound client: pooled
// connections, one log entry per request, hard timeout.
type fetcherSender struct {
	fetcher *fetcher.Fetcher
}

// NewFetcherSender adapts the shared fetcher to the delivery contract.
func NewFetcherSender(f *fetcher.Fetcher) Sender {
	return fetcherSender{fetcher: f}
}

// Send performs the request with an explicit method and header set and
// returns the receiver's status and body. A non-2xx status is data for
// the caller, not a transport error: the retry decision belongs to the
// queue, which knows the attempt budget.
func (s fetcherSender) Send(ctx context.Context, delivery OutboundDelivery) (int, []byte, error) {
	status, body, err := s.fetcher.SendRaw(ctx, delivery.Method, delivery.URL, delivery.Headers, delivery.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("webhook: deliver: %w", err)
	}
	return status, body, nil
}
