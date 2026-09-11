package identity

import (
	"context"
	"errors"
	"strconv"
	"sync"
)

// AuditEvent is what identity emits to a recorder. Declared here (not
// imported from auditlog) so modules stay decoupled.
type AuditEvent struct {
	Action string
	Actor  string
	Target string
}

// ErrInvalidName is returned when a user payload fails validation.
var ErrInvalidName = errors.New("name is required")

// Store abstracts user persistence. Swap in a database-backed
// implementation without touching handlers or business rules.
type Store interface {
	List(ctx context.Context) []User
	Create(ctx context.Context, name string) (User, error)
	GetByID(ctx context.Context, id string) (User, bool)
}

// User is the identity module's core entity.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MemoryStore is an in-memory Store, guarded by a mutex.
type MemoryStore struct {
	mu     sync.Mutex
	users  []User
	nextID int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{nextID: 1}
}

func (s *MemoryStore) List(_ context.Context) []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]User, len(s.users))
	copy(out, s.users)
	return out
}

func (s *MemoryStore) Create(_ context.Context, name string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user := User{ID: strconv.Itoa(s.nextID), Name: name}
	s.nextID++
	s.users = append(s.users, user)
	return user, nil
}

func (s *MemoryStore) GetByID(_ context.Context, id string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.ID == id {
			return u, true
		}
	}
	return User{}, false
}

// Service holds the identity business rules.
type Service struct {
	store    Store
	recorder Recorder
}

// Recorder receives audit events from identity. It is a function type
// so the composition root can adapt any sink (e.g. the auditlog
// module) without identity importing that module.
type Recorder func(event AuditEvent)

func NewService(store Store, recorder Recorder) *Service {
	return &Service{store: store, recorder: recorder}
}

func (s *Service) List(ctx context.Context) []User {
	return s.store.List(ctx)
}

func (s *Service) GetByID(ctx context.Context, id string) (User, error) {
	user, ok := s.store.GetByID(ctx, id)
	if !ok {
		return User{}, errors.New("user not found")
	}
	return user, nil
}

func (s *Service) Create(ctx context.Context, name string) (User, error) {
	if name == "" {
		return User{}, ErrInvalidName
	}

	user, err := s.store.Create(ctx, name)
	if err != nil {
		return User{}, err
	}

	if s.recorder != nil {
		s.recorder(AuditEvent{Action: "user.created", Actor: user.ID, Target: user.ID})
	}
	return user, nil
}
