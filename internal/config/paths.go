package config

import "path/filepath"

// Runtime files (logs, backups, keys) live under App.DataDir;
// one setting relocates all state.
const (
	dataLogsDir    = "logs"
	dataBackupDir  = "backup"
	dataKeysDir    = "keys"
	dataLogFile    = "app.log"
	dataPrivateKey = "private_key.pem"
	dataPublicKey  = "public_key.pem"
)

// DataDir returns the data root (default fills when unset).
func (c *Config) DataDir() string {
	if c.App.DataDir != "" {
		return c.App.DataDir
	}
	return defaultConfig.App.DataDir
}

// LogFile is the file-sink path under the data root.
func (c *Config) LogFile() string {
	return filepath.Join(c.DataDir(), dataLogsDir, dataLogFile)
}

// BackupDir is the dump/export dir under the data root.
func (c *Config) BackupDir() string {
	return filepath.Join(c.DataDir(), dataBackupDir)
}

// KeysDir is the generated-keys dir under the data root.
func (c *Config) KeysDir() string {
	return filepath.Join(c.DataDir(), dataKeysDir)
}

// PrivateKeyPath is the generated private-key PEM path.
func (c *Config) PrivateKeyPath() string {
	return filepath.Join(c.KeysDir(), dataPrivateKey)
}

// PublicKeyPath is the generated public-key PEM path.
func (c *Config) PublicKeyPath() string {
	return filepath.Join(c.KeysDir(), dataPublicKey)
}
