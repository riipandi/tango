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

type SecretsCmd struct {
	// Apply writes to this env file; it is distinct from global --env-file.
	Apply   bool   `help:"Update the env file with new secrets"`
	RSA     bool   `help:"Generate JWT keys using RSA algorithm (2048-bit)"`
	MLDSA   bool   `help:"Generate post-quantum ML-DSA-87 JWT keys (FIPS 204)"`
	OutFile string `name:"out" default:".env.local" help:"Env file to update when using --apply"`
}

func (s *SecretsCmd) Help() string {
	return fmt.Sprintf("\nExamples:\n"+
		"  %[1]s secrets\n"+
		"  %[1]s secrets --apply\n"+
		"  %[1]s secrets --out .env.staging --apply\n"+
		"  %[1]s secrets --rsa\n"+
		"  %[1]s secrets --mldsa\n", config.AppName)
}

func (s *SecretsCmd) Run(cli *CLI) error {
	outFile, err := sanitizeOutputPath(s.OutFile, "env file")
	if err != nil {
		return err
	}

	if s.Apply {
		info, statErr := os.Stat(outFile)
		if statErr != nil || info.IsDir() {
			fmt.Printf("%sERROR: %s not found, the --apply flag requires %s to exist%s\n",
				colorRed, outFile, outFile, colorReset)
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
		return applySecrets(outFile, keys)
	}
	printSecrets(keys)
	return nil
}

type secretsBundle struct {
	display [][2]string
	env     [][2]string
}

// generateSecrets builds random secrets plus the key pair
// (PEM on disk, base64 DER for env rows).
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
	rows := [][2]string{
		{"APP_SECRET_KEY", appSecret},
		{"JWT_PRIVATE_KEY", privateKey},
		{"JWT_PUBLIC_KEY", publicKey},
		{"JWT_SECRET_KEY", jwtSecret},
	}
	return secretsBundle{display: rows, env: rows}, nil
}

func printSecrets(keys secretsBundle) {
	fmt.Printf("%sApplication Secrets:%s\n", colorBold, colorReset)
	for _, kv := range keys.display {
		fmt.Printf("%s=%s\n", kv[0], kv[1])
	}
}

// sanitizeOutputPath cleans an operator-supplied output path and
// rejects traversal that escapes the working directory via leading
// ".." components (gosec G703). Absolute paths stay allowed:
// operators may target any location explicitly.
func sanitizeOutputPath(path, label string) (string, error) {
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s path %q must not traverse outside the working directory", label, path)
	}
	return cleaned, nil
}

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

// upsertEnvFile replaces the key or appends it. Kept at 0600:
// the file holds secrets.
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

	// #nosec G703 -- envFile is the operator-supplied --out target,
	// sanitized by sanitizeOutputPath (clean + traversal rejection)
	// and verified to exist as a regular file before this call.
	return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func loadSecretsConfig(cli *CLI) (*config.Config, error) {
	return loadConfig(cli, nil)
}

func randomBase64Key() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// generateJWTKeyPair writes PEM files, returns base64 DER pair.
// Precedence: --mldsa > --rsa > Ed25519 default.
func generateJWTKeyPair(outDir string, useRSA, useMLDSA bool) (string, string, error) {
	derPrivate, derPublic, err := marshalKeyPair(useRSA, useMLDSA)
	if err != nil {
		return "", "", err
	}
	if err := writeKeyPair(outDir, derPrivate, derPublic); err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(derPrivate),
		base64.StdEncoding.EncodeToString(derPublic),
		nil
}

func marshalKeyPair(useRSA, useMLDSA bool) (derPrivate, derPublic []byte, err error) {
	switch {
	case useMLDSA:
		key, genErr := mldsa.GenerateKey(mldsa.MLDSA87())
		if genErr != nil {
			return nil, nil, genErr
		}
		derPrivate, err = x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, nil, err
		}
		derPublic, err = x509.MarshalPKIXPublicKey(key.PublicKey())
		if err != nil {
			return nil, nil, err
		}
	case useRSA:
		key, genErr := rsa.GenerateKey(nil, 2048)
		if genErr != nil {
			return nil, nil, genErr
		}
		derPrivate, err = x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, nil, err
		}
		derPublic, err = x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return nil, nil, err
		}
	default:
		_, priv, genErr := ed25519.GenerateKey(nil)
		if genErr != nil {
			return nil, nil, genErr
		}
		derPrivate, err = x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return nil, nil, err
		}
		derPublic, err = x509.MarshalPKIXPublicKey(priv.Public())
		if err != nil {
			return nil, nil, err
		}
	}
	return derPrivate, derPublic, nil
}

// writeKeyPair stores both PEM files with secret-safe permissions.
func writeKeyPair(outDir string, derPrivate, derPublic []byte) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create keys dir: %w", err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: derPrivate})
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derPublic})
	if err := os.WriteFile(filepath.Join(outDir, "private_key.pem"), privatePEM, 0o600); err != nil {
		return err
	}
	// 0600: the whole key directory holds secret material; no
	// need for group/other read on the public half either.
	if err := os.WriteFile(filepath.Join(outDir, "public_key.pem"), publicPEM, 0o600); err != nil {
		return err
	}
	return nil
}
