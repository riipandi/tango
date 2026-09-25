package user

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"uuid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/responder"
)

// The failures the account procedures report. The handler maps them to
// connect codes, so the wire form of a refusal lives with the transport,
// not here.
var (
	// ErrUserNotFound is a read, update, or delete whose identifier names
	// no account.
	ErrUserNotFound = errors.New("user: account not found")

	// ErrAccountExists covers a taken username and a taken email. The
	// username is matched case-insensitively, the way its unique index is.
	ErrAccountExists = errors.New("user: account already exists")

	// ErrSelfDeletion is the refusal of the one deletion an administrator
	// cannot perform: their own signed-in account.
	ErrSelfDeletion = errors.New("user: cannot delete the signed-in account")
)

// Service administers the accounts.
type Service struct {
	pool   *datastore.Postgres
	repo   *Repository
	hasher *crypto.PasswordHasher
	log    *slog.Logger
	now    func() time.Time
}

// NewService builds the service. The database writes run in one transaction
// the service opens over the pool, so an account and its credential commit
// together or not at all.
func NewService(pool *datastore.Postgres, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		pool:   pool,
		repo:   NewRepository(),
		hasher: crypto.NewPasswordHasher(),
		log:    log,
		now:    time.Now,
	}
}

// CreateParams carries one administrator-created account.
type CreateParams struct {
	Username      string
	Email         string
	Password      string // empty means the account carries no credential yet
	FirstName     string
	LastName      string
	DisplayName   string
	Locale        string
	IsAdmin       bool
	Disabled      bool
	EmailVerified bool
}

// UpdateParams carries the replacement fields of one account. The ban fields
// are one unit: BanExpiresAt present applies the ban — the start instant is
// recorded here, keeping an earlier one — absent, it lifts it.
type UpdateParams struct {
	ID           uuid.UUID
	Username     string
	Email        string
	FirstName    string
	LastName     string
	DisplayName  string
	Locale       string
	IsAdmin      bool
	Disabled     bool
	BanExpiresAt *time.Time
	BanReason    *string
}

// UserView is an account as the procedures answer it: the fields a client
// renders or an operator manages, never the credential hash.
type UserView struct {
	ID            string
	Username      string
	Email         string
	DisplayName   string
	FirstName     *string
	LastName      *string
	Locale        *string
	IsAdmin       bool
	Disabled      bool
	EmailVerified bool
	CreatedAt     time.Time
	BannedAt      *time.Time
	BanExpires    *time.Time
	BanReason     *string
}

// CreateUser creates an account directly, without a signup token. The
// optional password is hashed and stored with the account; absent, the
// account carries no credential until a later procedure sets one.
func (s *Service) CreateUser(ctx context.Context, params CreateParams) (UserView, error) {
	passwordHash := ""
	if params.Password != "" {
		hash, err := s.hasher.Hash(params.Password)
		if err != nil {
			return UserView{}, fmt.Errorf("user: hash password: %w", err)
		}
		passwordHash = hash
	}

	displayName := params.DisplayName
	if displayName == "" {
		displayName = DisplayName(params.FirstName, params.LastName, params.Username)
	}

	var emailVerifiedAt *time.Time
	if params.EmailVerified {
		at := s.now()
		emailVerifiedAt = &at
	}

	var created UserView
	err := s.pool.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		id, createErr := s.repo.CreateUser(ctx, tx, UserSchema{
			Username:        params.Username,
			Email:           params.Email,
			FirstName:       params.FirstName,
			LastName:        params.LastName,
			DisplayName:     displayName,
			Locale:          params.Locale,
			IsAdmin:         params.IsAdmin,
			Disabled:        params.Disabled,
			EmailVerifiedAt: emailVerifiedAt,
			CreatedAt:       s.now(),
		})
		if errUniqueViolation(createErr) {
			return ErrAccountExists
		}
		if createErr != nil {
			return fmt.Errorf("user: create: %w", createErr)
		}

		if passwordHash != "" {
			if passErr := s.repo.CreatePassword(ctx, tx, id, passwordHash); passErr != nil {
				return passErr
			}
		}

		// The database fills the columns the insert omits (the creation
		// instant among them), so the view is read back rather than
		// assembled from the request.
		row, readErr := s.repo.GetUser(ctx, tx, id)
		if readErr != nil {
			return readErr
		}

		created = view(row)
		return nil
	})
	if err != nil {
		return UserView{}, err
	}
	return created, nil
}

// GetUser answers one account by its identifier.
func (s *Service) GetUser(ctx context.Context, id string) (UserView, error) {
	userID, err := uuid.Parse(id)
	if err != nil {
		return UserView{}, ErrUserNotFound
	}
	row, err := s.repo.GetUser(ctx, s.pool, userID)
	if errors.Is(err, datastore.ErrNoRows) {
		return UserView{}, ErrUserNotFound
	}
	if err != nil {
		return UserView{}, err
	}
	return view(row), nil
}

// ListUsers answers one page of the accounts, newest first, optionally
// filtered by a search term.
func (s *Service) ListUsers(ctx context.Context, search string, page, limit int) ([]UserView, responder.Pagination, error) {
	page, limit = responder.NormalizePage(page, limit, responder.DefaultPageSize, responder.MaxPageSize)

	rows, total, err := s.repo.ListUsers(ctx, s.pool, search, responder.Offset(page, limit), limit)
	if err != nil {
		return nil, responder.Pagination{}, err
	}

	views := make([]UserView, 0, len(rows))
	for _, row := range rows {
		views = append(views, view(row))
	}
	return views, responder.NewPagination(responder.PaginationParams{Page: page, Limit: limit}, total), nil
}

// UpdateUser replaces an account's writable fields. The account is read
// first: it is the not-found check, and it supplies the ban's start instant
// the automatic recording keeps.
func (s *Service) UpdateUser(ctx context.Context, id string, params UpdateParams) (UserView, error) {
	userID, err := uuid.Parse(id)
	if err != nil {
		return UserView{}, ErrUserNotFound
	}
	existing, err := s.repo.GetUser(ctx, s.pool, userID)
	if errors.Is(err, datastore.ErrNoRows) {
		return UserView{}, ErrUserNotFound
	}
	if err != nil {
		return UserView{}, err
	}

	row := UserSchema{
		ID:          userID,
		Username:    params.Username,
		Email:       params.Email,
		FirstName:   params.FirstName,
		LastName:    params.LastName,
		DisplayName: params.DisplayName,
		Locale:      params.Locale,
		IsAdmin:     params.IsAdmin,
		Disabled:    params.Disabled,
		// The creation instant is immutable; the update statement leaves the
		// column alone, and the answer carries the value as it stood.
		CreatedAt:  existing.CreatedAt,
		BannedAt:   existing.BannedAt,
		BanExpires: params.BanExpiresAt,
		BanReason:  params.BanReason,
	}
	if params.BanExpiresAt != nil {
		// The ban is applied now unless an earlier one is on record: the
		// start instant answers "since when", so a re-ban does not move it.
		if existing.BannedAt == nil {
			at := s.now()
			row.BannedAt = &at
		}
	} else {
		// Absent expiry lifts the ban as a unit, reason included.
		row.BannedAt = nil
		row.BanReason = nil
	}

	updated, err := s.repo.UpdateUser(ctx, s.pool, row)
	if errUniqueViolation(err) {
		return UserView{}, ErrAccountExists
	}
	if err != nil {
		return UserView{}, err
	}
	if !updated {
		return UserView{}, ErrUserNotFound
	}
	return view(row), nil
}

// DeleteUser removes an account. The caller's username travels with the
// request, because the one deletion an administrator cannot perform is the
// account they are signed in with — the identifier is compared
// case-insensitively, the way the username column matches.
func (s *Service) DeleteUser(ctx context.Context, id, callerUsername string) error {
	userID, err := uuid.Parse(id)
	if err != nil {
		return ErrUserNotFound
	}
	row, err := s.repo.GetUser(ctx, s.pool, userID)
	if errors.Is(err, datastore.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(row.Username, callerUsername) {
		return ErrSelfDeletion
	}

	deleted, err := s.repo.DeleteUser(ctx, s.pool, userID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrUserNotFound
	}
	return nil
}

// view maps a stored row onto the account view.
func view(row UserSchema) UserView {
	return UserView{
		ID:            row.ID.String(),
		Username:      row.Username,
		Email:         row.Email,
		DisplayName:   row.DisplayName,
		FirstName:     optional(row.FirstName),
		LastName:      optional(row.LastName),
		Locale:        optional(row.Locale),
		IsAdmin:       row.IsAdmin,
		Disabled:      row.Disabled,
		EmailVerified: row.EmailVerifiedAt != nil,
		CreatedAt:     row.CreatedAt,
		BannedAt:      row.BannedAt,
		BanExpires:    row.BanExpires,
		BanReason:     row.BanReason,
	}
}

// optional hands the view a pointer for a column that may be NULL.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// DisplayName composes the name the UI shows from the optional given and
// family names, falling back to the username when neither is carried. The
// account-creating features share it, so a sign-up and an administrator
// creation name an account the same way.
func DisplayName(firstName, lastName, username string) string {
	name := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(firstName), strings.TrimSpace(lastName)}, " "))
	if name == "" {
		return username
	}
	return name
}
