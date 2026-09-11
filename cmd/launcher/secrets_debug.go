//go:build debug

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

	"github.com/spf13/cobra"
)

// ANSI colors, matching the shell scripts output style.
const (
	colorRed   = "\033[0;31m"
	colorGreen = "\033[0;32m"
	colorCyan  = "\033[0;36m"
	colorBold  = "\033[1m"
	colorReset = "\033[0m"
)

var (
	secretsApply   bool
	secretsRSA     bool
	secretsMLDSA   bool
	secretsEnvFile string
)

var secretsCmd = &cobra.Command{
	Use:          "secrets",
	Short:        "Generate application secrets",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if secretsApply {
			if _, err := os.Stat(secretsEnvFile); err != nil {
				fmt.Printf("%sERROR: %s not found, the --apply flag requires %s to exist%s\n",
					colorRed, secretsEnvFile, secretsEnvFile, colorReset)
				fmt.Printf("Create the file first, or run without --apply to generate secrets only\n\n")
				return nil
			}
		}

		appSecretKey, err := randomBase64Key()
		if err != nil {
			return fmt.Errorf("generate app secret: %w", err)
		}

		jwtSecretKey, err := randomBase64Key()
		if err != nil {
			return fmt.Errorf("generate jwt secret: %w", err)
		}

		privateKey, publicKey, err := generateJWTKeyPair(secretsRSA, secretsMLDSA)
		if err != nil {
			return fmt.Errorf("generate jwt key pair: %w", err)
		}

		if secretsApply {
			fmt.Printf("%sUpdating %s file...%s\n\n", colorBold, secretsEnvFile, colorReset)
			for _, kv := range [][2]string{
				{"APP_SECRET_KEY", appSecretKey},
				{"JWT_PRIVATE_KEY", privateKey},
				{"JWT_PUBLIC_KEY", publicKey},
				{"JWT_SECRET_KEY", jwtSecretKey},
			} {
				if err := upsertEnvFile(secretsEnvFile, kv[0], kv[1]); err != nil {
					return fmt.Errorf("update %s: %w", secretsEnvFile, err)
				}
				fmt.Printf("%s=%s\n", kv[0], kv[1])
			}
			fmt.Printf("\n%sEnvironment secrets updated successfully%s\n", colorGreen, colorReset)
			return nil
		}

		fmt.Printf("%sApplication Secrets:%s\n", colorBold, colorReset)
		fmt.Printf("APP_SECRET_KEY=%s\n", appSecretKey)
		fmt.Printf("JWT_PRIVATE_KEY=%s\n", privateKey)
		fmt.Printf("JWT_PUBLIC_KEY=%s\n", publicKey)
		fmt.Printf("JWT_SECRET_KEY=%s\n", jwtSecretKey)
		return nil
	},
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

// generateJWTKeyPair writes the PEM files to storage/ and returns the
// base64-encoded (DER, no PEM headers) private and public keys.
// Algorithm precedence: --mldsa (post-quantum ML-DSA-87, FIPS 204)
// > --rsa (RSA 2048) > Ed25519 (default).
func generateJWTKeyPair(useRSA, useMLDSA bool) (string, string, error) {
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
		key, err := rsa.GenerateKey(nil, 2048)
		if err != nil {
			return "", "", err
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
	outDir := filepath.Join("storage", "keys")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
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

	return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func init() {
	secretsCmd.Flags().BoolVar(&secretsApply, "apply", false, "Update the env file with new secrets")
	secretsCmd.Flags().BoolVar(&secretsRSA, "rsa", false, "Generate JWT keys using RSA algorithm (2048-bit)")
	secretsCmd.Flags().BoolVar(&secretsMLDSA, "mldsa", false, "Generate post-quantum ML-DSA-87 JWT keys (FIPS 204)")
	secretsCmd.Flags().StringVar(&secretsEnvFile, "env-file", ".env.local", "Env file to update when using --apply")

	secretsCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Printf("%sApplication Secrets Generator%s\n\n", colorBold, colorCyan)
		fmt.Printf("Usage:\n")
		fmt.Printf("  tango secrets                - Generate keys and display secrets\n")
		fmt.Printf("  tango secrets --apply        - Generate keys and apply to .env.local\n")
		fmt.Printf("  tango secrets --env-file .env.staging --apply\n")
		fmt.Printf("  tango secrets --rsa          - Generate RSA keys and display secrets\n")
		fmt.Printf("  tango secrets --rsa --apply  - Generate RSA keys and apply to .env.local\n")
		fmt.Printf("  tango secrets --mldsa        - Generate post-quantum ML-DSA-87 keys (FIPS 204)\n")
		fmt.Printf("\nOptions:\n")
		fmt.Printf("  --apply              Update the env file with new secrets\n")
		fmt.Printf("  --rsa                Generate JWT keys using RSA algorithm (2048-bit)\n")
		fmt.Printf("  --mldsa              Generate post-quantum ML-DSA-87 JWT keys (FIPS 204)\n")
		fmt.Printf("  --env-file <file>    Env file for --apply (default: .env.local)\n")
	})
	rootCmd.AddCommand(secretsCmd)
}
