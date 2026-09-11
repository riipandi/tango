package config

import "path/filepath"

// On-disk layout under App.DataDir. Every runtime file the binary
// writes — logs, backups, generated keys — lives below this root,
// so operators relocate all state with one setting (APP_DATA_DIR
// or --data-dir) instead of per-concern keys.
const (
	dataLogsDir    = "logs"
	dataBackupDir  = "backup"
	dataKeysDir    = "keys"
	dataLogFile    = "app.log"
	dataPrivateKey = "private_key.pem"
	dataPublicKey  = "public_key.pem"
)

// DataDir returns the configured data root (never empty: the
// default fills it when no layer overrides it).
func (c *Config) DataDir() string {
	if c.App.DataDir != "" {
		return c.App.DataDir
	}
	return defaultConfig.App.DataDir
}

// LogFile derives the file-sink path from the data root.
func (c *Config) LogFile() string {
	return filepath.Join(c.DataDir(), dataLogsDir, dataLogFile)
}

// BackupDir derives the dump/export directory from the data root.
func (c *Config) BackupDir() string {
	return filepath.Join(c.DataDir(), dataBackupDir)
}

// KeysDir derives the generated-keys directory from the data root.
func (c *Config) KeysDir() string {
	return filepath.Join(c.DataDir(), dataKeysDir)
}

// PrivateKeyPath derives the generated private-key PEM path.
func (c *Config) PrivateKeyPath() string {
	return filepath.Join(c.KeysDir(), dataPrivateKey)
}

// PublicKeyPath derives the generated public-key PEM path.
func (c *Config) PublicKeyPath() string {
	return filepath.Join(c.KeysDir(), dataPublicKey)
}
