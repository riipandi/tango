package launcher

import (
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/riipandi/tango/internal/config"
)

// SecretsCmd generates application secrets. Keys land under the
// configured data root (<data-dir>/keys). Available in every build
// variant: operators need it wherever they provision.
type SecretsCmd struct {
	// Out is the env file --apply writes to. It deliberately has a
	// different name from the global --env-file: one is an input
	// (config layering), the other an output (secret sink).
	Apply   bool   `help:"Update the env file with new secrets"`
	RSA     bool   `help:"Generate JWT keys using RSA algorithm (2048-bit)"`
	MLDSA   bool   `help:"Generate post-quantum ML-DSA-87 JWT keys (FIPS 204)"`
	OutFile string `name:"out" default:".env.local" help:"Env file to update when using --apply"`
}

// Help augments the command help with usage examples.
func (s *SecretsCmd) Help() string {
	return "\nExamples:\n" +
		"  tango secrets\n" +
		"  tango secrets --apply\n" +
		"  tango secrets --out .env.staging --apply\n" +
		"  tango secrets --rsa\n" +
		"  tango secrets --mldsa\n"
}

// Run generates keys and displays or applies the secrets.
func (s *SecretsCmd) Run(cli *CLI) error {
	if s.Apply {
		if _, err := os.Stat(s.OutFile); err != nil {
			fmt.Printf("%sERROR: %s not found, the --apply flag requires %s to exist%s\n",
				colorRed, s.OutFile, s.OutFile, colorReset)
			fmt.Printf("Create the file first, or run without --apply to generate secrets only\n\n")
			return nil
		}
	}

	cfg, err := loadSecretsConfig(cli)
	if err != nil {
		return err
	}

	keys, err := generateSecrets(cfg.KeysDir(), s.RSA, s.MLDSA)
	if err != nil {
		return err
	}

	if s.Apply {
		return applySecrets(s.OutFile, keys)
	}
	printSecrets(keys)
	return nil
}

// secretsBundle is one generated set: display values plus the
// env-file rows --apply writes.
type secretsBundle struct {
	display [][2]string
	env     [][2]string
}

// generateSecrets builds random app/JWT secrets plus the JWT key
// pair (PEM files on disk, base64 DER for the env rows).
func generateSecrets(keysDir string, useRSA, useMLDSA bool) (secretsBundle, error) {
	appSecret, err := randomBase64Key()
	if err != nil {
		return secretsBundle{}, fmt.Errorf("generate app secret: %w", err)
	}
	jwtSecret, err := randomBase64Key()
	if err != nil {
		return secretsBundle{}, fmt.Errorf("generate jwt secret: %w", err)
	}
	privateKey, publicKey, err := generateJWTKeyPair(keysDir, useRSA, useMLDSA)
	if err != nil {
		return secretsBundle{}, fmt.Errorf("generate jwt key pair: %w", err)
	}
	return secretsBundle{
		display: [][2]string{
			{"APP_SECRET_KEY", appSecret},
			{"JWT_PRIVATE_KEY", privateKey},
			{"JWT_PUBLIC_KEY", publicKey},
			{"JWT_SECRET_KEY", jwtSecret},
		},
		env: [][2]string{
			{"APP_SECRET_KEY", appSecret},
			{"JWT_PRIVATE_KEY", privateKey},
			{"JWT_PUBLIC_KEY", publicKey},
			{"JWT_SECRET_KEY", jwtSecret},
		},
	}, nil
}

// printSecrets renders the bundle to stdout.
func printSecrets(keys secretsBundle) {
	fmt.Printf("%sApplication Secrets:%s\n", colorBold, colorReset)
	for _, kv := range keys.display {
		fmt.Printf("%s=%s\n", kv[0], kv[1])
	}
}

// applySecrets writes the bundle into the env file, one key per line.
func applySecrets(outFile string, keys secretsBundle) error {
	fmt.Printf("%sUpdating %s file...%s\n\n", colorBold, outFile, colorReset)
	for _, kv := range keys.env {
		if err := upsertEnvFile(outFile, kv[0], kv[1]); err != nil {
			return fmt.Errorf("update %s: %w", outFile, err)
		}
		fmt.Printf("%s=%s\n", kv[0], kv[1])
	}
	fmt.Printf("\n%sEnvironment secrets updated successfully%s\n", colorGreen, colorReset)
	return nil
}

// loadSecretsConfig loads the layered configuration for the
// secrets command: only the data root matters (keys land under
// <data-dir>/keys). Global CLI flags (--data-dir) win over env
// and env-file layers.
func loadSecretsConfig(cli *CLI) (*config.Config, error) {
	return loadConfig(cli, nil)
}

// randomBase64Key returns a cryptographically secure 48-byte
// base64-encoded random string (64 chars).
func randomBase64Key() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// generateJWTKeyPair writes the PEM files to outDir and returns the
// base64-encoded (DER, no PEM headers) private and public keys.
// Algorithm precedence: --mldsa (post-quantum ML-DSA-87, FIPS 204)
// > --rsa (RSA 2048) > Ed25519 (default).
func generateJWTKeyPair(outDir string, useRSA, useMLDSA bool) (string, string, error) {
	var derPrivate, derPublic []byte
	var err error

	switch {
	case useMLDSA:
		key, genErr := mldsa.GenerateKey(mldsa.MLDSA87())
		if genErr != nil {
			return "", "", genErr
		}
		derPrivate, err = x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return "", "", err
		}
		derPublic, err = x509.MarshalPKIXPublicKey(key.PublicKey())
		if err != nil {
			return "", "", err
		}
	case useRSA:
		key, genErr := rsa.GenerateKey(nil, 2048)
		if genErr != nil {
			return "", "", genErr
		}
		derPrivate, err = x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return "", "", err
		}
		derPublic, err = x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return "", "", err
		}
	default:
		_, priv, genErr := ed25519.GenerateKey(nil)
		if genErr != nil {
			return "", "", genErr
		}
		derPrivate, err = x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return "", "", err
		}
		derPublic, err = x509.MarshalPKIXPublicKey(priv.Public())
		if err != nil {
			return "", "", err
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create keys dir: %w", err)
	}

	privatePath := filepath.Join(outDir, "private_key.pem")
	publicPath := filepath.Join(outDir, "public_key.pem")

	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: derPrivate})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derPublic})

	if err := os.WriteFile(privatePath, privatePEM, 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(publicPath, publicPEM, 0o644); err != nil {
		return "", "", err
	}

	return base64.StdEncoding.EncodeToString(derPrivate),
		base64.StdEncoding.EncodeToString(derPublic),
		nil
}

// upsertEnvFile replaces the value when the key exists, appends otherwise.
// The file holds secrets, so it is created (and kept) at 0600.
func upsertEnvFile(envFile, key, value string) error {
	data, err := os.ReadFile(envFile)
	if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	seen := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = key + "=" + value
			seen = true
			break
		}
	}
	if !seen {
		lines = append(lines, key+"="+value)
	}

	return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}
