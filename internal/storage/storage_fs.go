package storage

// TODO: filesystem backend (dev / single-node).
//
// Root from config. Put: reject keys escaping root (no "..",
// no absolute), MkdirAll parent, write temp + rename (atomic).
// Open via os.Root or ErrNotFound; Delete removes, prunes empty
// parents best-effort.
