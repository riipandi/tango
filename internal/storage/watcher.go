package storage

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches the staging directory and settles each path: a file that
// has stayed quiet for the debounce window has been written whole, and its
// upload may be enqueued. It is what turns a file landing on disk into one
// durable task, however many small writes produced it.
//
// The watcher is a component of a serve run: Start runs until the context is
// cancelled, then the watch is closed and the loop exits, so a drain never
// leaves an event handler behind.
type Watcher struct {
	dir      string
	debounce time.Duration
	// onSettle is called with the file's key — its path relative to the
	// staging directory — once the path has gone quiet. It must not block:
	// the loop runs it inline, and a slow callback would delay every
	// other settle.
	onSettle func(key string)
}

// NewWatcher builds the staging watcher.
func NewWatcher(dir string, debounce time.Duration, onSettle func(key string)) *Watcher {
	return &Watcher{dir: dir, debounce: debounce, onSettle: onSettle}
}

// Start runs the watch until ctx is cancelled. It creates the staging
// directory if it is missing — a run whose staging area is empty has never
// needed one — and settles the files already sitting there, so an upload
// that outlived its process is picked up by the next one. The caller runs it
// on its own goroutine; the returned error is the reason the watch ended.
func (w *Watcher) Start(ctx context.Context) error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}

	watch, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = watch.Close() }()

	if err := watch.Add(w.dir); err != nil {
		return err
	}

	// The staging files that predate this run settle immediately, and the
	// debounce map starts empty: a scan hit and a live event may name the
	// same file, and the second window only delays the settle by the
	// debounce time.
	w.scan()

	timers := make(map[string]*time.Timer)
	settled := make(chan string, 64)
	for {
		select {
		case <-ctx.Done():
			for _, timer := range timers {
				timer.Stop()
			}
			return nil

		case event, ok := <-watch.Events:
			if !ok {
				return nil
			}
			key, ok := w.stageKey(event.Name)
			if !ok {
				continue
			}
			if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) &&
				!event.Has(fsnotify.Rename) && !event.Has(fsnotify.Remove) {
				continue
			}
			w.resetTimer(ctx, timers, settled, key)

		case err, ok := <-watch.Errors:
			if !ok {
				return nil
			}
			slog.WarnContext(ctx, "storage: watch error", "err", err)

		case key := <-settled:
			// The timer fires on its own goroutine and only sends; the map
			// is touched here and nowhere else, so there is no lock.
			delete(timers, key)
			w.onSettle(key)
		}
	}
}

// resetTimer pushes a path's settle back by the debounce window, creating
// the timer the first event names. The window is what makes a file written
// in many small writes one upload instead of one per write.
func (w *Watcher) resetTimer(ctx context.Context, timers map[string]*time.Timer, settled chan<- string, key string) {
	if timer, pending := timers[key]; pending {
		timer.Reset(w.debounce)
		return
	}
	timers[key] = time.AfterFunc(w.debounce, func() {
		select {
		case settled <- key:
		case <-ctx.Done():
		}
	})
}

// scan settles what the staging directory already holds, the recovery path
// for an upload whose process died before the manifest was committed.
func (w *Watcher) scan() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		w.onSettle(entry.Name())
	}
}

// stageKey reads the key a path inside the staging directory holds. A path
// outside the directory — fsnotify names absolute paths, an event may name
// the directory itself — is not a staging file.
func (w *Watcher) stageKey(path string) (string, bool) {
	rel, err := filepath.Rel(w.dir, path)
	if err != nil || strings.HasPrefix(rel, ".") || strings.Contains(rel, string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}
