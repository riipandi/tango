package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"
)

// GeneratedKey is a freshly minted key pair, ready to persist. The
// private PEM is stored encrypted by the service; the public PEM is
// published verbatim.
type GeneratedKey struct {
	ID          JWKID
	KeyID       string
	Algorithm   string
	KeyType     string
	PublicPEM   []byte
	PrivatePEM  []byte
	ActivatedAt time.Time
}

// GenerateKey creates a key pair for algorithm (RS256 or ES256) and
// assigns a unique kid derived from the row ID. kid is minted once
// here and never changes across restarts.
func GenerateKey(algorithm string) (*GeneratedKey, error) {
	id := NewID()
	key := &GeneratedKey{
		ID:          id,
		KeyID:       id.String(),
		Algorithm:   algorithm,
		ActivatedAt: time.Now().UTC(),
	}

	switch algorithm {
	case RS256:
		key.KeyType = KeyTypeRSA
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate RSA key: %w", err)
		}
		pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("marshal RSA public key: %w", err)
		}
		key.PublicPEM = pemEncode("PUBLIC KEY", pubDER)
		privDER, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return nil, fmt.Errorf("marshal RSA private key: %w", err)
		}
		key.PrivatePEM = pemEncode("PRIVATE KEY", privDER)
	case ES256:
		key.KeyType = KeyTypeEC
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate EC key: %w", err)
		}
		pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("marshal EC public key: %w", err)
		}
		key.PublicPEM = pemEncode("PUBLIC KEY", pubDER)
		privDER, err := x509.MarshalECPrivateKey(priv)
		if err != nil {
			return nil, fmt.Errorf("marshal EC private key: %w", err)
		}
		key.PrivatePEM = pemEncode("EC PRIVATE KEY", privDER)
	default:
		return nil, fmt.Errorf("unsupported signing algorithm %q", algorithm)
	}
	return key, nil
}

// parsePrivatePEM decodes a stored private PEM into a crypto key.
func parsePrivatePEM(pemBytes []byte) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("decode private key PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// parsePublicPEM decodes a stored public PEM into a crypto key.
func parsePublicPEM(pemBytes []byte) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("decode public key PEM")
	}
	return x509.ParsePKIXPublicKey(block.Bytes)
}

func pemEncode(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}
