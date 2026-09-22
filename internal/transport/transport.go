package transport

// rateLimitExclusions are the API path prefixes the rate limiter never
// counts. One entry per line, the reason beside it. A prefix matches the
// paths under it, so "/api/healthz" also spares "/api/healthz/deep"; the
// limiter itself runs on the API surface only, so a static asset or a
// metrics scrape never reaches a check in the first place.
var rateLimitExclusions = []string{
	"/api/healthz", // liveness probes and load-balancer checks
}
