package totp

// totp.go wraps github.com/pquerna/otp for RFC 6238 verification:
// seed generation, the otpauth provisioning link, and step-window
// verification with a replay watermark. The HTTP layer stays out.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

// Fixed parameters of the tango enrollment profile. Authenticator
// defaults (SHA-1, 6 digits, 30 seconds) maximize app compatibility;
// the skew is bounded to one step on either side.
const (
	AlgorithmSHA1   = "SHA1"
	AlgorithmSHA256 = "SHA256"
	AlgorithmSHA512 = "SHA512"

	defaultDigits = 6
	defaultPeriod = 30
	defaultSkew   = 1

	// seedBytes is 160 bits, the RFC-recommended secret length.
	seedBytes = 20
)

// ErrInvalidCode rejects a wrong code, a replayed step, and an
// out-of-window code alike.
var ErrInvalidCode = fmt.Errorf("totp: code is invalid or expired")

// b32 is the RFC 4648 base32 alphabet without padding, the form
// authenticator apps expect when a secret is typed by hand.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSeed returns a fresh base32 secret.
func GenerateSeed() (string, error) {
	buf := make([]byte, seedBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("totp: seed: %w", err)
	}
	return b32.EncodeToString(buf), nil
}

// ProvisioningURI renders the otpauth:// link authenticator apps
// scan. The issuer rides both the label and the query parameter.
func ProvisioningURI(issuer, account, secret string, digits, period int, algorithm string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", algorithm)
	query.Set("digits", strconv.Itoa(digits))
	query.Set("period", strconv.Itoa(period))
	return "otpauth://totp/" + label + "?" + query.Encode()
}

// otpDigits maps the enrollment digit count to the library type.
func otpDigits(digits int) (otp.Digits, error) {
	switch digits {
	case 6:
		return otp.DigitsSix, nil
	case 8:
		return otp.DigitsEight, nil
	default:
		return 0, fmt.Errorf("totp: unsupported digit count %d", digits)
	}
}

// otpAlgorithm maps the stored algorithm name to the library type.
func otpAlgorithm(algorithm string) (otp.Algorithm, error) {
	switch algorithm {
	case AlgorithmSHA1:
		return otp.AlgorithmSHA1, nil
	case AlgorithmSHA256:
		return otp.AlgorithmSHA256, nil
	case AlgorithmSHA512:
		return otp.AlgorithmSHA512, nil
	default:
		return 0, fmt.Errorf("totp: unknown algorithm %q", algorithm)
	}
}

// VerifyCode checks code against the secret around the current step
// (±skew steps), rejects any step at or before lastUsedStep, and
// returns the accepted step for replay protection.
func VerifyCode(secret, code string, digits, period int, algorithm string, skew, lastUsedStep int64, now time.Time) (int64, error) {
	d, err := otpDigits(digits)
	if err != nil {
		return 0, err
	}
	alg, err := otpAlgorithm(algorithm)
	if err != nil {
		return 0, err
	}

	normalized := strings.TrimSpace(code)
	if len(normalized) != digits || !isDigits(normalized) {
		return 0, ErrInvalidCode
	}

	step := now.Unix() / int64(period)
	for candidate := step + skew; candidate >= step-skew; candidate-- {
		if candidate <= lastUsedStep {
			// Older steps are spent; later candidates in the window
			// still count.
			continue
		}
		if candidate < 0 {
			// Steps before the epoch are unreachable; keep the
			// counter conversion total.
			continue
		}
		computed, err := deriveCode(secret, uint64(candidate), d, alg)
		if err != nil {
			return 0, fmt.Errorf("totp: derive code: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(computed), []byte(normalized)) == 1 {
			return candidate, nil
		}
	}
	return 0, ErrInvalidCode
}

// deriveCode wraps the library's counter-based derivation.
func deriveCode(secret string, counter uint64, digits otp.Digits, algorithm otp.Algorithm) (string, error) {
	return hotp.GenerateCodeCustom(secret, counter, hotp.ValidateOpts{
		Digits:    digits,
		Algorithm: algorithm,
	})
}

// isDigits reports whether s is ASCII digits only.
func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
