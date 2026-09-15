package config

import "path/filepath"

// Runtime files live under App.DataDir.
const (
	dataLogsDir    = "logs"
	dataBackupDir  = "backup"
	dataKeysDir    = "keys"
	dataLogFile    = "app.log"
	dataPrivateKey = "private_key.pem"
	dataPublicKey  = "public_key.pem"
)

// DataDir returns the configured data root.
func (c *Config) DataDir() string {
	if c.App.DataDir != "" {
		return c.App.DataDir
	}
	return defaultConfig.App.DataDir
}

// LogFile returns the application log path.
func (c *Config) LogFile() string {
	return filepath.Join(c.DataDir(), dataLogsDir, dataLogFile)
}

// BackupDir returns the backup directory.
func (c *Config) BackupDir() string {
	return filepath.Join(c.DataDir(), dataBackupDir)
}

// KeysDir returns the generated key directory.
func (c *Config) KeysDir() string {
	return filepath.Join(c.DataDir(), dataKeysDir)
}

// PrivateKeyPath returns the private key path.
func (c *Config) PrivateKeyPath() string {
	return filepath.Join(c.KeysDir(), dataPrivateKey)
}

// PublicKeyPath returns the public key path.
func (c *Config) PublicKeyPath() string {
	return filepath.Join(c.KeysDir(), dataPublicKey)
}
