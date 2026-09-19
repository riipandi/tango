// Package appconfig serves public and admin configuration endpoints.
package appconfig

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName identifies the appconfig module in the registry.
const ModuleName = "appconfig"

// Module mounts the configuration endpoints.
type Module struct {
	store       Store
	mailer      MailSender
	envDefaults map[string]string
	cipher      *crypto.Cipher
}

// MailSender queues transactional email; internal/jobs implements it.
type MailSender interface {
	EnqueueEmail(ctx context.Context, msg mailer.Message) error
}

// New builds the module. A nil mailer leaves the test-email route
// unmounted (fail closed); the config CRUD needs WithStore.
func New(mailer MailSender, opts ...Option) *Module {
	m := &Module{mailer: mailer}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithStore wires the settings persistence; without it only
// test-email mounts.
func (m *Module) WithStore(store Store) *Module {
	m.store = store
	return m
}

// Option configures the appconfig module at construction.
type Option func(*Module)

// WithEnvDefaults seeds the env-backed defaults (MAILER_*
// values from the koanf config). Catalog defaults stay the bottom
// layer; DB overrides stay the top.
func (m *Module) WithEnvDefaults(defaults map[string]string) *Module {
	m.envDefaults = defaults
	return m
}

// WithCipher wires the seal for sensitive settings; without one the
// module rejects writes to sensitive keys and returns stored
// ciphertext untouched on reads (fail closed).
func (m *Module) WithCipher(cipher *crypto.Cipher) *Module {
	m.cipher = cipher
	return m
}

// Name identifies the module.
func (*Module) Name() string { return ModuleName }

// APIRoutes mounts the retained public bootstrap endpoint relative
// to /api. The admin CRUD and test-email surfaces serve ConnectRPC
// exclusively (see handler_rpc.go).
func (m *Module) APIRoutes(r chi.Router) {
	if m.store != nil {
		r.Get("/application-configuration", m.listPublic)
	}
}

// listPublic serves GET /application-configuration: the settings the
// unauthenticated SPA may see (env defaults folded with DB overrides).
func (m *Module) listPublic(w http.ResponseWriter, r *http.Request) {
	overrides, err := m.store.List(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	merged, err := m.merged(r.Context(), overrides)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, publicView(merged))
}

// merged folds the module's env defaults with the stored overrides,
// revealing the encrypted sensitive values.
func (m *Module) merged(ctx context.Context, overrides map[string]string) (map[string]string, error) {
	plain, err := m.decryptOverrides(overrides)
	if err != nil {
		return nil, err
	}
	return mergedValues(m.envDefaults, plain), nil
}

// MergedValues folds env defaults with stored overrides — the
// cross-module read path (the registry wires the CIMD allowlist from
// it).
func (m *Module) MergedValues(ctx context.Context) (map[string]string, error) {
	overrides, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	return m.merged(ctx, overrides)
}

// decryptOverrides reveals sensitive stored values. An undecryptable
// value (wrong key, tampering) is a configuration error: it fails
// closed instead of leaking ciphertext or falling back to plaintext.
func (m *Module) decryptOverrides(overrides map[string]string) (map[string]string, error) {
	if m.cipher == nil {
		for key, value := range overrides {
			if entry, known := lookup(key); known && entry.Sensitive && value != "" {
				return nil, fmt.Errorf("sensitive setting %s cannot be revealed: no cipher configured", key)
			}
		}
		return overrides, nil
	}

	out := make(map[string]string, len(overrides))
	for key, value := range overrides {
		if entry, known := lookup(key); known && entry.Sensitive && value != "" {
			plain, err := m.cipher.Decrypt(value)
			if err != nil {
				return nil, fmt.Errorf("sensitive setting %s cannot be revealed: %w", key, err)
			}
			value = plain
		}
		out[key] = value
	}
	return out, nil
}

// sealSensitive encrypts every sensitive value in place. Empty
// values never reach this path: the update handler routes them to
// the store's Delete (clearing).
func (m *Module) sealSensitive(values map[string]string) error {
	if m.cipher == nil {
		for key, value := range values {
			if entry, known := lookup(key); known && entry.Sensitive && value != "" {
				return fmt.Errorf("sensitive setting %s cannot be sealed: no cipher configured", key)
			}
		}
		return nil
	}
	for key, value := range values {
		entry, known := lookup(key)
		if !known || !entry.Sensitive || value == "" {
			continue
		}
		sealed, err := m.cipher.Encrypt(value)
		if err != nil {
			return fmt.Errorf("sensitive setting %s cannot be sealed: %w", key, err)
		}
		values[key] = sealed
	}
	return nil
}
