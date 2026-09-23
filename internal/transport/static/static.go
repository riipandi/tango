// Package static serves the files a feature wrote under the data directory's
// uploads subdirectory, at the route prefix app.assets_url points at.
//
// It lives under transport because it is part of the HTTP surface, beside
// middleware: it is a mount the router composes, not a service a module owns,
// and nothing outside transport imports it. It is separate from
// web.SetupStatic, which serves the compiled SPA: the two answer different
// questions, and keeping them apart is what lets this one be a plain file
// server with no fallback — a missing upload is a 404, never an HTML page.
package static

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
)

// Path is the route prefix the uploads are served under. It is the path
// app.assets_url defaults to, so a value written there and a file written
// under the data directory meet at the same URL.
const Path = "/static"

// Handler serves dir at Path.
//
// A missing directory is not an error: a run that has never stored an upload
// has none, and the route answers 404 for every path under it rather than
// failing the run. The directory is created on the first write, by whatever
// stores the upload.
//
// The request path is resolved against dir by os.DirFS, which refuses to leave
// it: a ".." segment or an absolute path is rejected by the filesystem before
// any file is opened, so a request cannot read outside the uploads directory.
func Handler(dir string) http.Handler {
	// The directory is opened per request through DirFS, which resolves a
	// relative name inside it. A name that would escape is an error from the
	// filesystem itself, not a check this handler has to keep in step.
	fsys := os.DirFS(dir)
	files := http.FileServerFS(fsys)

	return http.StripPrefix(Path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The uploads are immutable by convention — a stored file's name
		// carries its identity — so a client may cache one for as long as it
		// likes without a revalidation round trip. The path is what names the
		// content, so a replacement lands under a new name.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	}))
}

// Mount registers the handler on the router.
func Mount(r chi.Router, dir string) {
	r.Handle(Path, Handler(dir))
	r.Handle(Path+"/*", Handler(dir))
}

// Dir returns the uploads directory under the given data directory.
func Dir(dataDir string) string {
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, config.UploadsDir)
}
