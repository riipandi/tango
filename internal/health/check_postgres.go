package health

import (
	"context"
	"fmt"
	"time"
)

// CheckNamePostgres is the name of the database check in a result.
const CheckNamePostgres = "postgres"

// DefaultPostgresTimeout bounds the database probe. It is shorter than the
// checker timeout so a database that hangs still leaves room to report it.
const DefaultPostgresTimeout = 2 * time.Second

// Pinger is the part of a connection pool a Postgres check needs.
// *datastore.Postgres satisfies it, and so does a fake in a test.
type Pinger interface {
	Ping(ctx context.Context) error
}

// PostgresCheck reports whether the Postgres pool can reach the database.
//
// Ping uses a connection from the pool, so this also fails while the pool is
// exhausted, which is the state a saturated service is in.
func PostgresCheck(pool Pinger) Check {
	return Check{
		Name:    CheckNamePostgres,
		Timeout: DefaultPostgresTimeout,
		Check: func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("postgres: %w", err)
			}
			return nil
		},
	}
}
