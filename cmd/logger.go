package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
)

// # The process logger

// loggerState holds the logger for this run, built on first use.
//
// It is built lazily because the configuration may not resolve yet: the root
// Before runs before every command, including the two that bootstrap a fresh
// checkout — config:generate writes the file, and key:generate runs before the
// variables the file references exist. A command that needs neither the
// configuration nor logging must not fail because a logger could not be built.
type loggerState struct {
	once sync.Once

	log *logger.Logger
	err error
	cfg config.Config
}

// loggerFrom returns the process logger, building it on first call.
//
// A failure is returned rather than swallowed: a command that asked for a logger
// and did not get one should say so, because the alternative is a run that looks
// healthy and writes nowhere.
func loggerFrom(ctx context.Context) (*logger.Logger, error) {
	state, ok := ctx.Value(loggerKey{}).(*loggerState)
	if !ok {
		return nil, errors.New("logger: not installed for this command")
	}

	state.once.Do(func() {
		state.log, state.err = logger.New(state.cfg)
		if state.err == nil {
			state.log.SetDefault()
		}
	})
	return state.log, state.err
}

// loggerKey is the context key the logger state is stored under. It is a private
// struct type so no other package can collide with it.
type loggerKey struct{}

// installLogger builds the logger the configuration describes and puts it on the
// context, so every command that logs lands in the same pipeline.
//
// The configuration is resolved here rather than through configFrom, because a
// failure at this point is not the command's problem: the state keeps the error
// and the command that actually needs logging reports it.
func installLogger(ctx context.Context, cmd *cli.Command) context.Context {
	cfg, err := configFrom(ctx)
	if err != nil {
		// The configuration did not resolve, so there is nothing to build a
		// logger from. The state is still installed, holding the failure, so a
		// command that asks for one is told why rather than getting a nil.
		return context.WithValue(ctx, loggerKey{}, &loggerState{err: err})
	}
	return context.WithValue(ctx, loggerKey{}, &loggerState{cfg: cfg})
}

// closeLogger flushes the logger at the end of a run. It is a no-op when no
// command ever built one, which is the common case for the bootstrap commands.
//
// The flush gets its own deadline rather than the run's context. A run that was
// cancelled — a signal, a deadline — reaches here with a context that is already
// done, and the collector's queue is the one thing that still has to be drained:
// handing a cancelled context to it would drop every queued record at exactly
// the moment a service is being shut down. The deadline is what bounds the
// flush instead.
func closeLogger(ctx context.Context) error {
	state, ok := ctx.Value(loggerKey{}).(*loggerState)
	if !ok || state.log == nil {
		return nil
	}

	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), loggerShutdownTimeout)
	defer cancel()
	return state.log.Shutdown(flush)
}

// loggerShutdownTimeout bounds the final flush. It is short because a shutdown
// path is one a caller is waiting on, and long enough for one batch to leave.
const loggerShutdownTimeout = 5 * time.Second
