package storage

// TODO: filesystem backend (development / single-node deploys).
//
// Root directory comes from config (e.g. storage.fs.root). On Put:
//
//   - resolve the key under the root with filepath security —
//     reject any key that escapes the root (no "..", no absolute
//     paths);
//   - MkdirAll the parent, then write to a temp file and rename
//     for atomic replacement.
//
// Open returns os.Root-opened file or ErrNotFound; Delete removes
// the file (and prunes empty parent dirs, best effort).
