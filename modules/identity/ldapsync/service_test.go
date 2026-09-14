package ldapsync

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-ldap/ldap/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/pkg/testutils"
)

// fakeLDAP scripts Search results by filter (user vs group search) and
// can fail bind or search.
type fakeLDAP struct {
	users  *ldap.SearchResult
	groups *ldap.SearchResult
	// extra serves base-object lookups by DN (member resolution).
	extra map[string]*ldap.SearchResult

	bindErr   error
	searchErr error
	closed    int
}

func (f *fakeLDAP) Bind(_, _ string) error { return f.bindErr }

func (f *fakeLDAP) Close() error { f.closed++; return nil }

func (f *fakeLDAP) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	switch req.Filter {
	case "(objectClass=person)":
		return f.users, nil
	case "(objectClass=groupOfNames)":
		return f.groups, nil
	case "(objectClass=*)":
		if result, ok := f.extra[req.BaseDN]; ok {
			return result, nil
		}
		return &ldap.SearchResult{}, nil
	default:
		return &ldap.SearchResult{}, nil
	}
}

// ldapEntry builds an LDAP entry from string attributes.
func ldapEntry(dn string, attrs map[string]string) *ldap.Entry {
	values := make(map[string][]string, len(attrs))
	for k, v := range attrs {
		values[k] = []string{v}
	}
	return ldap.NewEntry(dn, values)
}

// settings is a valid enabled config pointing at a dead address (the
// dialer is replaced in every test that reaches it).
func settings() LDAPSettings {
	return LDAPSettings{
		Enabled:        true,
		URL:            "ldap://127.0.0.1:1",
		Base:           "dc=tango,dc=test",
		AdminGroupName: "admins",
	}
}

// newSyncService builds the service over the shared Postgres container
// with a fake dialer; the raw datastore comes back for assertions.
func newSyncService(t *testing.T, fake *fakeLDAP) (*Service, *datastore.Postgres) {
	t.Helper()
	pg := testutils.StartPostgres(t.Context(), t)
	if _, err := database.MigrateUp(t.Context(), pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(t.Context(), datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	svc := NewService(ds, logger.Slog(logger.NewMock()))
	return svc.WithDialer(func(LDAPSettings) (ldapClient, error) { return fake, nil }), ds
}

// directory returns a fake directory with two users, one regular
// group, and the admin group. Usernames are unique per run: the
// container DB is shared across the binary.
func directory(t *testing.T) (*fakeLDAP, string, string) {
	t.Helper()
	suffix := stamp()
	ada := ldapEntry("uid=ada"+suffix+",ou=people,dc=tango,dc=test", map[string]string{
		"uid":         "ada" + suffix,
		"mail":        "ada." + suffix + "@tango.test",
		"givenName":   "Ada",
		"sn":          "Wong",
		"displayName": "Ada Wong",
	})
	bob := ldapEntry("uid=bob"+suffix+",ou=people,dc=tango,dc=test", map[string]string{
		"uid":  "bob" + suffix,
		"mail": "bob." + suffix + "@tango.test",
		"sn":   "Builder",
	})
	devs := ldapEntry("cn=devs"+suffix+",ou=groups,dc=tango,dc=test", map[string]string{
		"cn":     "devs" + suffix,
		"member": "uid=ada" + suffix + ",ou=people,dc=tango,dc=test",
	})
	admins := ldapEntry("cn=admins"+suffix+",ou=groups,dc=tango,dc=test", map[string]string{
		"cn":     "admins" + suffix,
		"member": "uid=ada" + suffix + ",ou=people,dc=tango,dc=test",
	})
	fake := &fakeLDAP{
		users:  &ldap.SearchResult{Entries: []*ldap.Entry{ada, bob}},
		groups: &ldap.SearchResult{Entries: []*ldap.Entry{devs, admins}},
	}
	return fake, "ada" + suffix, "admins" + suffix
}

func TestSyncAllReconcilesDirectory(t *testing.T) {
	ctx := t.Context()
	fake, adaName, adminName := directory(t)
	svc, ds := newSyncService(t, fake)
	cfg := settings()
	cfg.AdminGroupName = adminName

	stats, err := svc.SyncAll(ctx, cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.UsersCreated)
	assert.Equal(t, 2, stats.GroupsCreated)

	// Idempotent re-run: nothing changes.
	stats, err = svc.SyncAll(ctx, cfg)
	require.NoError(t, err)
	assert.Zero(t, stats.UsersCreated)
	assert.Zero(t, stats.GroupsCreated)

	// The admin group flag reached the user row.
	var isAdmin bool
	require.NoError(t, ds.QueryRow(ctx,
		"SELECT is_admin FROM public.users WHERE username = $1", adaName).Scan(&isAdmin))
	assert.True(t, isAdmin, "admin group membership grants the admin flag")
}

func TestSyncAllErrors(t *testing.T) {
	fake, _, _ := directory(t)
	svc, ds := newSyncService(t, fake)
	ctx := t.Context()

	// Disabled and unconfigured settings fail fast, without dialing.
	disabled := settings()
	disabled.Enabled = false
	_, err := svc.SyncAll(ctx, disabled)
	assert.ErrorIs(t, err, ErrDisabled)

	unconfigured := settings()
	unconfigured.URL = ""
	_, err = svc.SyncAll(ctx, unconfigured)
	assert.ErrorIs(t, err, ErrNotConfigured)

	// Dial failure surfaces as-is (real dialer against a dead address).
	realDial := NewService(ds, logger.Slog(logger.NewMock()))
	_, err = realDial.SyncAll(ctx, settings())
	assert.Error(t, err)

	// Search failure wraps the LDAP error.
	failing, _ := newSyncService(t, &fakeLDAP{searchErr: errors.New("boom")})
	_, err = failing.SyncAll(ctx, settings())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch state")
}

func TestResolveMemberPaths(t *testing.T) {
	ctx := t.Context()
	fake, adaName, _ := directory(t)
	svc, _ := newSyncService(t, fake)

	byDN := map[string]string{normalizeDN("uid=" + adaName + ",ou=people,dc=tango,dc=test"): adaName}

	// 1. DN cache hit.
	assert.Equal(t, adaName, svc.resolveMember(ctx, fake,
		"uid="+adaName+",ou=people,dc=tango,dc=test", byDN, "uid"))

	// 2. DN carrying the username attribute: extracted directly.
	assert.Equal(t, adaName, svc.resolveMember(ctx, fake,
		"uid="+adaName+",ou=people,dc=other", map[string]string{}, "uid"))

	// 3. DN without the attribute: base-object lookup.
	fake.extra = map[string]*ldap.SearchResult{
		"cn=hidden,ou=people,dc=tango,dc=test": {
			Entries: []*ldap.Entry{ldapEntry("cn=hidden", map[string]string{"uid": "hidden" + adaName})},
		},
	}
	assert.Equal(t, "hidden"+adaName, svc.resolveMember(ctx, fake,
		"cn=hidden,ou=people,dc=tango,dc=test", map[string]string{}, "uid"))

	// 4. Unresolvable member: empty.
	assert.Equal(t, "", svc.resolveMember(ctx, fake,
		"cn=nobody,dc=tango,dc=test", map[string]string{}, "uid"))
}

func TestSanitizeUsername(t *testing.T) {
	assert.Equal(t, "ada_wong", sanitizeUsername("Ada Wong"))
	assert.Equal(t, "ada_wong_x_1", sanitizeUsername("Ada.Wong X-1"), "separators all become underscores")
	assert.Equal(t, "trimmed", sanitizeUsername("  Trimmed  "))
	assert.Equal(t, "", sanitizeUsername(""))
}

func TestNormalizeDN(t *testing.T) {
	// Attribute types lowercase, values stay as-is: the same DN always
	// maps to the same key.
	assert.Equal(t, "cn=Ada,ou=People", normalizeDN("CN=Ada, OU=People"))
	assert.Equal(t, "weird dn", normalizeDN("Weird DN"), "unparsable DNs fall back to lowercase")
	assert.Equal(t, "", normalizeDN(""))
}

func TestDNProperty(t *testing.T) {
	assert.Equal(t, "ada", dnProperty("uid", "uid=ada,ou=people"))
	assert.Equal(t, "people", dnProperty("OU", "uid=ada,OU=people"))
	assert.Equal(t, "", dnProperty("cn", "uid=ada,ou=people"))
	assert.Equal(t, "", dnProperty("uid", "not a dn"))
}

func TestSettingsWithDefaults(t *testing.T) {
	filled := LDAPSettings{UserFilter: "(custom)"}.withDefaults()
	assert.Equal(t, "(custom)", filled.UserFilter)
	assert.Equal(t, "(objectClass=groupOfNames)", filled.GroupFilter)
	assert.Equal(t, "uid", filled.AttrUserUniqueID)
	assert.Equal(t, "mail", filled.AttrUserEmail)
	assert.Equal(t, "givenName", filled.AttrUserFirstName)
	assert.Equal(t, "sn", filled.AttrUserLastName)
	assert.Equal(t, "displayName", filled.AttrUserDisplay)
	assert.Equal(t, "cn", filled.AttrGroupUniqueID)
	assert.Equal(t, "member", filled.AttrGroupMember)
}

func TestFetchPicture(t *testing.T) {
	svc, _ := newSyncService(t, &fakeLDAP{})
	ctx := t.Context()

	_, err := svc.FetchPicture(ctx, "not a url")
	assert.Error(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("png-bytes"))
	}))
	defer srv.Close()
	picture, err := svc.FetchPicture(ctx, srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "png-bytes", string(picture))

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()
	_, err = svc.FetchPicture(ctx, missing.URL)
	assert.Error(t, err)
}
