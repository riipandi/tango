package static

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// Local serves uploads from a directory on this machine.
type Local struct {
	fsys fs.FS
}

// NewLocal serves the uploads under dir.
//
// A missing directory is not an error: a run that never stored an upload has
// none, and the mount answers 404 for every key until the first write creates
// it. The directory is created by whatever stores the upload, never here.
func NewLocal(dir string) *Local {
	return &Local{fsys: os.DirFS(dir)}
}

// Serve answers one request from the file the key names.
//
// The key is resolved against the directory by os.DirFS, which is what refuses
// a ".." element or an absolute path: the escape is rejected before a file is
// opened, so this driver carries no traversal check of its own to keep in step
// with the filesystem.
func (l *Local) Serve(_ context.Context, w http.ResponseWriter, r *http.Request, key string) error {
	// A backslash is the one separator os.DirFS accepts as a path character —
	// it is a separator only on a platform where it would leave the root, and
	// fs.ValidPath does not reject it there.
	if strings.ContainsRune(key, '\\') {
		return ErrNotFound
	}

	file, err := l.fsys.Open(key)
	if err != nil {
		// An unknown name and a name that would leave the root are one answer:
		// a client learns nothing about the tree from the difference.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			return ErrNotFound
		}
		return fmt.Errorf("static: open %q: %w", key, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("static: stat %q: %w", key, err)
	}

	// A directory is not an upload. Serving one would list the keys under it —
	// the names of files the client was never given — so it is a miss, like
	// any other key that names no file.
	if info.IsDir() {
		return ErrNotFound
	}

	// The file is streamed, never read whole: ServeContent honours a range
	// request and a conditional one, so a large upload is fetched in pieces.
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		return fmt.Errorf("static: %q is not readable", key)
	}
	http.ServeContent(w, r, key, info.ModTime(), seeker)
	return nil
}
