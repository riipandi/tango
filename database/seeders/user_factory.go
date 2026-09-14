// Package seeders builds database fixtures: the first-admin setup
// row and deterministic development users. It is standalone — SQL
// against the documented schema plus pkg/crypto for password
// hashing; no identity-module imports.
package seeders

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/riipandi/tango/pkg/crypto"
)

// Schema locations owned by database/migrations; duplicating the
// names here keeps the seeder free of module imports.
const (
	usersTable       = "public.users"
	userPasswordsTab = "public.user_passwords"
)

// Executor is the datastore.Executor surface the seeder needs.
// QueryRow returns pgx.Row directly — Go return-type invariance
// means a concrete pgx.Row cannot satisfy a custom Scanner iface.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// AdminParams carries the setup-admin fields.
type AdminParams struct {
	Username string
	Email    string
	Password string
}

// SetupAdmin creates the first admin account with a hashed password.
// It fails when any user already exists — the setup flow is only
// valid on a fresh database. Input validation runs before the
// freshness gate so misconfiguration is reported precisely.
func SetupAdmin(ctx context.Context, db Executor, params AdminParams) (string, error) {
	if err := validateAdmin(params); err != nil {
		return "", err
	}

	exists, err := HasAnyUser(ctx, db)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("seeders: database is not fresh, setup refused")
	}

	return insertUser(ctx, db, params, true)
}

// validateAdmin mirrors the users-table constraints up front.
func validateAdmin(params AdminParams) error {
	params.Username = strings.ToLower(strings.TrimSpace(params.Username))
	params.Email = strings.TrimSpace(params.Email)
	if len(params.Username) < 3 || len(params.Username) > 32 {
		return fmt.Errorf("seeders: username must be 3-32 characters")
	}
	for _, r := range params.Username {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'
		if !ok {
			return fmt.Errorf("seeders: username must be alphanumeric or underscore")
		}
	}
	if !strings.Contains(params.Email, "@") || strings.ContainsAny(params.Email, " \t") {
		return fmt.Errorf("seeders: invalid email")
	}
	if len(params.Password) < 8 {
		return fmt.Errorf("seeders: password must be at least 8 characters")
	}
	return nil
}

// insertUser writes the row and its password hash; returns the UUID.
func insertUser(ctx context.Context, db Executor, params AdminParams, isAdmin bool) (string, error) {
	display := strings.TrimSpace(params.Username)
	hash, err := crypto.NewPasswordHasher().Hash(params.Password)
	if err != nil {
		return "", fmt.Errorf("seeders: hash password: %w", err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(usersTable)
	ib.Cols("username", "email", "display_name", "is_admin", "email_verified_at")
	ib.Values(params.Username, params.Email, display, isAdmin, sqlbuilder.Raw("now()"))
	ib.SQL("RETURNING id")
	query, args := ib.Build()

	var uuidText string
	if err := db.QueryRow(ctx, query, args...).Scan(&uuidText); err != nil {
		return "", fmt.Errorf("seeders: create user %q: %w", params.Username, err)
	}

	ub := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ub.InsertInto(userPasswordsTab)
	ub.Cols("user_id", "password_hash")
	ub.Values(uuidText, hash)
	ub.SQL("ON CONFLICT (user_id) DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now()")
	pwQuery, pwArgs := ub.Build()
	if _, err := db.Exec(ctx, pwQuery, pwArgs...); err != nil {
		return "", fmt.Errorf("seeders: store password: %w", err)
	}
	return uuidText, nil
}

// HasAnyUser reports whether the accounts table is non-empty — the
// freshness gate for the setup flow.
func HasAnyUser(ctx context.Context, db Executor) (bool, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("exists(SELECT 1 FROM " + usersTable + ")")
	query, args := sb.Build()
	var exists bool
	if err := db.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("seeders: check users: %w", err)
	}
	return exists, nil
}

// suffixMod bounds the numeric suffix (name-xxxx).
const suffixMod = 10000

// UserFactory creates deterministic development users (base_NNNN)
// with throwaway hashed passwords. Standalone: plain SQL plus
// pkg/crypto. Safe for concurrent use.
type UserFactory struct {
	db Executor

	mu   sync.Mutex
	next int
}

// NewUserFactory builds a factory over db. An explicit seed sets the
// starting suffix (mod 10000); zero selects a crypto-randomized
// start for ad-hoc runs.
func NewUserFactory(db Executor, seed uint64) *UserFactory {
	if seed == 0 {
		if n, err := rand.Int(rand.Reader, big.NewInt(suffixMod)); err == nil {
			seed = n.Uint64()
		}
	}
	return &UserFactory{db: db, next: int(seed % suffixMod)}
}

// Create builds one user with the next sequential suffix, wrapping
// past 9999 back to 0000. Passwords are the username plus a fixed
// dev suffix — hashed, never stored in plaintext.
func (f *UserFactory) Create(ctx context.Context, base string) (string, error) {
	if base == "" {
		base = "user"
	}
	f.mu.Lock()
	n := f.next
	f.next = (f.next + 1) % suffixMod
	f.mu.Unlock()

	name := fmt.Sprintf("%s_%04d", sanitizeBase(base), n)
	id, err := insertUser(ctx, f.db, AdminParams{
		Username: name,
		Email:    name + "@users.local",
		Password: name + "-devpass",
	}, false)
	if err != nil {
		return "", fmt.Errorf("seed user %q: %w", name, err)
	}
	return id, nil
}

// sanitizeBase keeps only username-safe characters; the factory must never fail on fixture names.
func sanitizeBase(base string) string {
	out := make([]rune, 0, len(base))
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "user"
	}
	return string(out)
}

// CreateMany builds count users sharing the same base name, returning their UUIDs in creation order.
func (f *UserFactory) CreateMany(ctx context.Context, base string, count int) ([]string, error) {
	ids := make([]string, 0, count)
	for range count {
		id, err := f.Create(ctx, base)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
