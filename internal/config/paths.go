package config

// DefaultDataDir is where the application keeps local files, relative to the
// working directory. It is the default of the --data-dir flag and of the
// storage health check, and it matches the compose volume (./storage:/srv/storage).
const DefaultDataDir = "storage"
