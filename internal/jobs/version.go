package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/logger"
)

// VersionFeed caches the newest published release so /api/version/latest
// answers from memory instead of calling GitHub on every request.
type VersionFeed struct {
	// Fetch performs the lookup; injected so it can be stubbed in tests.
	Fetch func(ctx context.Context) (string, error)

	// CheckInterval is how often the recurring job refreshes the value.
	CheckInterval time.Duration

	mu      sync.RWMutex
	latest  string
	fetched time.Time
}

// VersionCheckInterval is the refresh cadence; upstream caches for 15
// minutes, which is far more traffic than a release feed needs.
const VersionCheckInterval = 6 * time.Hour

// Latest returns the cached release, falling back to the running build
// when the feed has never succeeded: the endpoint must always answer.
func (v *VersionFeed) Latest() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.latest == "" {
		return config.AppVersion
	}
	return v.latest
}

// FetchedAt reports when the cached value was last refreshed.
func (v *VersionFeed) FetchedAt() time.Time {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.fetched
}

// Refresh performs one lookup and caches a non-empty result.
func (v *VersionFeed) Refresh(ctx context.Context) error {
	if v.Fetch == nil {
		return nil
	}
	version, err := v.Fetch(ctx)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if version == "" {
		return errors.New("jobs: version lookup returned an empty tag")
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	v.latest = version
	v.fetched = now()
	return nil
}

// NewVersionFeed builds the feed over the shared outbound client,
// reading the latest release tag from GitHub.
func NewVersionFeed(client *fetcher.Fetcher, source string) *VersionFeed {
	return &VersionFeed{
		CheckInterval: VersionCheckInterval,
		Fetch: func(ctx context.Context) (string, error) {
			var payload struct {
				TagName string `json:"tag_name"`
			}
			if _, err := client.GetJSON(ctx, source, &payload); err != nil {
				return "", err
			}
			return payload.TagName, nil
		},
	}
}

// VersionJob builds the recurring job that refreshes the feed.
func VersionJob(feed *VersionFeed, log logger.Logger) Job {
	return Job{
		Name:     "check_latest_version",
		Interval: VersionCheckInterval,
		Run: func(ctx context.Context) error {
			if err := feed.Refresh(ctx); err != nil {
				return fmt.Errorf("jobs: version check: %w", err)
			}
			log.Info(fmt.Sprintf("jobs: latest version is %s", feed.Latest()))
			return nil
		},
	}
}
