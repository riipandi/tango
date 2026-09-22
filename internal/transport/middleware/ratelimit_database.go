package middleware

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
)

// sqlStateLimited is the SQLSTATE the check function raises when a client has
// spent its window. The error is the refusal, so the driver reads it as the
// result rather than as a failure.
const sqlStateLimited = "42901"

// DatabaseLimiter runs the fixed-window check in the database, one call per
// request, the counter and its window synchronized under advisory locks.
//
// The database is the default backend because a rate limiter that needs no
// second server is one every deployment has; Valkey replaces it when a run
// names the kvstore driver.
type DatabaseLimiter struct {
	pool   datastore.Querier
	limit  int
	window time.Duration
}

// NewDatabaseLimiter builds the limiter over the shared pool. The pool is not
// held exclusively: the check runs on whatever the pool hands out, the way a
// repository's queries do.
func NewDatabaseLimiter(pool datastore.Querier, cfg config.RateLimit) *DatabaseLimiter {
	return &DatabaseLimiter{pool: pool, limit: cfg.Limit, window: cfg.Window}
}

// Allow runs the check function. A limited client comes back as a result, not
// as an error, so the middleware's failure path stays reserved for a backend
// that cannot answer at all.
func (l *DatabaseLimiter) Allow(ctx context.Context, key string) (Result, error) {
	windowSeconds := max(int(l.window/time.Second), 1)

	// The check is one function call, built the way every table query is:
	// the placeholders are sqlbuilder's, so the arguments never sit in the
	// statement by hand.
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("fn_check_rate_limit(" + sb.Var(key) + ", " + sb.Var(l.limit) + ", " + sb.Var(windowSeconds) + ")")
	query, args := sb.Build()

	var raw []byte
	err := l.pool.QueryRow(ctx, query, args...).Scan(&raw)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == sqlStateLimited {
		return Result{
			Limited:    true,
			Limit:      l.limit,
			Remaining:  0,
			RetryAfter: retryAfterFromDetail(pgErr.Detail, l.window),
		}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: check: %w", err)
	}

	var reply struct {
		Remaining int     `json:"remaining"`
		Reset     float64 `json:"reset"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return Result{}, fmt.Errorf("ratelimit: check: %w", err)
	}
	return Result{
		Limit:     l.limit,
		Remaining: reply.Remaining,
		// The epoch the function answers carries the fractional seconds an
		// EXTRACT keeps, so the float is truncated where it lands.
		ResetAt: time.Unix(int64(reply.Reset), 0),
	}, nil
}
