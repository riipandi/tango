package totp

// totp_test.go proves the RFC 6238 path with the standard test
// vectors and drives the lifecycle over the real Postgres store.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// TestRFC6238Vectors pins the algorithm against the RFC 6238 test
// vectors: ASCII secret "12345678901234567890", 8 digits, SHA-1.
func TestRFC6238Vectors(t *testing.T) {
	secret := b32.EncodeToString([]byte("12345678901234567890"))

	for _, tc := range []struct {
		unix int64
		code string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
	} {
		now := time.Unix(tc.unix, 0)
		accepted, err := VerifyCode(secret, tc.code, 8, 30, AlgorithmSHA1, 0, -1, now)
		require.NoError(t, err, tc.code)
		assert.Equal(t, tc.unix/30, accepted, tc.code)

		// A tampered code never passes.
		_, err = VerifyCode(secret, "00000000", 8, 30, AlgorithmSHA1, 0, -1, now)
		assert.ErrorIs(t, err, ErrInvalidCode, tc.code)
	}
}

func TestVerifyCodeWindowAndReplay(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	secret, err := GenerateSeed()
	require.NoError(t, err)

	accepted, err := VerifyCode(secret, codeAt(t, secret, now), defaultDigits, defaultPeriod, AlgorithmSHA1, defaultSkew, -1, now)
	require.NoError(t, err)

	// The same step is a replay; an older step is behind the
	// watermark; the next step inside the skew window is fine.
	_, err = VerifyCode(secret, codeAt(t, secret, now), defaultDigits, defaultPeriod, AlgorithmSHA1, defaultSkew, accepted, now)
	assert.ErrorIs(t, err, ErrInvalidCode, "the same step must not verify twice")

	_, err = VerifyCode(secret, codeAt(t, secret, now.Add(-time.Duration(defaultPeriod)*time.Second)),
		defaultDigits, defaultPeriod, AlgorithmSHA1, defaultSkew, accepted, now)
	assert.ErrorIs(t, err, ErrInvalidCode, "an older step must not verify after a newer one")

	_, err = VerifyCode(secret, codeAt(t, secret, now.Add(time.Duration(defaultPeriod)*time.Second)),
		defaultDigits, defaultPeriod, AlgorithmSHA1, defaultSkew, accepted, now)
	require.NoError(t, err, "the next step inside the skew window verifies")
}

// codeAt derives the live code for the given moment.
func codeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := codeFor(secret, at.Unix()/int64(defaultPeriod))
	require.NoError(t, err)
	return code
}

// codeFor derives one code through the same library path VerifyCode
// uses.
func codeFor(secret string, step int64) (string, error) {
	digits, err := otpDigits(defaultDigits)
	if err != nil {
		return "", err
	}
	algorithm, err := otpAlgorithm(AlgorithmSHA1)
	if err != nil {
		return "", err
	}
	return deriveCode(secret, uint64(step), digits, algorithm)
}

func TestLifecycleOverPostgres(t *testing.T) {
	ctx := t.Context()
	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	cipher, err := crypto.NewCipher(make([]byte, 32))
	require.NoError(t, err)

	users := user.NewPostgresStore(ds)
	store := NewPostgresStore(ds)
	sessions := sessionStub{issued: make([]string, 0, 1)}
	passwords := &passwordStub{secret: "correct-horse"}
	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")

	created, err := users.Create(ctx, user.CreateParams{
		Username: "totp_" + stamp,
		Email:    "totp-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	svc := NewService(store, users, &sessions, passwords, cipher, "Tango", nil,
		WithClock(func() time.Time { return time.Now().UTC() }))

	// Enrollment returns the seed exactly once and seals it at rest.
	secret, uri, err := svc.Enroll(ctx, created)
	require.NoError(t, err)
	assert.Contains(t, uri, "otpauth://totp/Tango:")
	assert.Contains(t, uri, "secret="+secret)

	row, err := store.State(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(row.SecretEnc, "enc:"), "the seed is sealed with the canonical prefix")
	opened, err := cipher.Decrypt(row.SecretEnc)
	require.NoError(t, err)
	assert.Equal(t, secret, opened)

	// Confirm rejects a wrong code, then verifies the live one and
	// issues exactly eight recovery codes.
	_, err = svc.Confirm(ctx, created, "000000")
	assert.ErrorIs(t, err, ErrInvalidCode)

	codes, err := svc.Confirm(ctx, created, codeAt(t, secret, time.Now().UTC()))
	require.NoError(t, err)
	require.Len(t, codes, recoveryCodeCount)

	confirmed, remaining, err := svc.Status(ctx, created)
	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Equal(t, recoveryCodeCount, remaining)

	// A confirmed enrollment blocks re-enrollment.
	_, _, err = svc.Enroll(ctx, created)
	assert.ErrorIs(t, err, ErrAlreadyConfirmed)

	// Recovery codes burn once; the remaining count follows.
	spent, err := store.ConsumeRecoveryCode(ctx, created.ID, recoveryHash(codes[0]))
	require.NoError(t, err)
	assert.True(t, spent)
	spent, err = store.ConsumeRecoveryCode(ctx, created.ID, recoveryHash(codes[0]))
	require.NoError(t, err)
	assert.False(t, spent, "a burned code never re-verifies")

	_, remaining, err = svc.Status(ctx, created)
	require.NoError(t, err)
	assert.Equal(t, recoveryCodeCount-1, remaining)

	// The pending bridge resolves to the user once, then never again.
	pendingRaw := "pending-bridge-token"
	require.NoError(t, store.PutPending(ctx, created.ID, hashToken(pendingRaw), PendingTTL))
	resolved, err := store.ConsumePending(ctx, hashToken(pendingRaw))
	require.NoError(t, err)
	assert.Equal(t, created.ID, resolved)
	_, err = store.ConsumePending(ctx, hashToken(pendingRaw))
	assert.Error(t, err, "the bridge is single-use")

	// Disable with the wrong password fails; the right one clears
	// every row.
	assert.ErrorIs(t, svc.Disable(ctx, created, "wrong-password"), ErrInvalidCode)
	require.NoError(t, svc.Disable(ctx, created, "correct-horse"))
	confirmed, _, err = svc.Status(ctx, created)
	require.NoError(t, err)
	assert.False(t, confirmed)
}

// passwordStub verifies against one plaintext secret; the lifecycle
// test does not depend on the credential package.
type passwordStub struct{ secret string }

func (p *passwordStub) VerifyForUser(ctx context.Context, userID user.UserID, secret string) error {
	if p.secret != secret {
		return errors.New("password stub: mismatch")
	}
	return nil
}

// sessionStub records issued session tokens.
type sessionStub struct{ issued []string }

func (s *sessionStub) IssueForUser(ctx context.Context, userID user.UserID, provider string, meta session.Meta) (string, error) {
	token := "issued-" + userID.String()
	s.issued = append(s.issued, token)
	return token, nil
}
