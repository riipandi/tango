//go:build !debug

package launcher

import "github.com/alecthomas/kong"

// dbOptions registers nothing in release builds: the db backup and
// restore group is a development tool that shells out to local
// PostgreSQL client binaries against repository-relative paths.
func dbOptions() []kong.Option { return nil }
