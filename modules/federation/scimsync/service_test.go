package scimsync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/logger"
)

// fakeSource is a mutable snapshot source.
type fakeSource struct {
	users    []ScimUserRow
	groups   []ScimGroupRow
	userErr  error
	groupErr error
}

func (f *fakeSource) UsersForClient(context.Context, string) ([]ScimUserRow, error) {
	return f.users, f.userErr
}

func (f *fakeSource) GroupsForClient(context.Context, string) ([]ScimGroupRow, error) {
	return f.groups, f.groupErr
}

// scimStub is a minimal remote SCIM provider: it stores created
// resources, answers list/create/update/delete, and can be told to
// misbehave for the error paths.
type scimStub struct {
	srv *httptest.Server

	users  map[string]ScimUser // keyed by remote id
	groups map[string]ScimGroup

	mut          sync.Mutex
	requests     []string // "METHOD /path" per request
	bearers      []string // Authorization header per request
	failList     bool     // 500 on every list
	failDelete   bool     // 500 on every delete
	rateLimitOne bool     // answer the next request with 429 + Retry-After: 1
	nextID       int
}

func newSCIMStub(t *testing.T) *scimStub {
	t.Helper()
	s := &scimStub{
		users:  map[string]ScimUser{},
		groups: map[string]ScimGroup{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *scimStub) serve(w http.ResponseWriter, r *http.Request) {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	s.bearers = append(s.bearers, r.Header.Get("Authorization"))

	if s.rateLimitOne {
		s.rateLimitOne = false
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	id := pathID(r.URL.Path)
	switch {
	case s.failList && r.Method == http.MethodGet:
		w.WriteHeader(http.StatusInternalServerError)
	case r.Method == http.MethodGet && r.URL.Path == "/Users":
		s.listUsers(w)
	case r.Method == http.MethodGet && r.URL.Path == "/Groups":
		s.listGroups(w)
	case r.Method == http.MethodPost && r.URL.Path == "/Users":
		var payload ScimUser
		if s.decode(w, r, &payload) {
			s.nextID++
			payload.ID = fmt.Sprintf("remote-%03d", s.nextID)
			s.users[payload.ID] = payload
			s.reply(w, http.StatusCreated, payload)
		}
	case r.Method == http.MethodPost && r.URL.Path == "/Groups":
		var payload ScimGroup
		if s.decode(w, r, &payload) {
			s.nextID++
			payload.ID = fmt.Sprintf("remote-%03d", s.nextID)
			s.groups[payload.ID] = payload
			s.reply(w, http.StatusCreated, payload)
		}
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/Users/"):
		if _, ok := s.users[id]; ok {
			var payload ScimUser
			if s.decode(w, r, &payload) {
				payload.ID = id
				s.users[id] = payload
				s.reply(w, http.StatusOK, payload)
			}
			return
		}
		http.NotFound(w, r)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/Groups/"):
		if _, ok := s.groups[id]; ok {
			var payload ScimGroup
			if s.decode(w, r, &payload) {
				payload.ID = id
				s.groups[id] = payload
				s.reply(w, http.StatusOK, payload)
			}
			return
		}
		http.NotFound(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/Users/"):
		s.delete(w, id)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/Groups/"):
		s.delete(w, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *scimStub) listUsers(w http.ResponseWriter) {
	values := make([]ScimUser, 0, len(s.users))
	for _, u := range s.users {
		values = append(values, u)
	}
	s.reply(w, http.StatusOK, ListResponse[ScimUser]{Resources: values, TotalResults: len(values)})
}

func (s *scimStub) listGroups(w http.ResponseWriter) {
	values := make([]ScimGroup, 0, len(s.groups))
	for _, g := range s.groups {
		values = append(values, g)
	}
	s.reply(w, http.StatusOK, ListResponse[ScimGroup]{Resources: values, TotalResults: len(values)})
}

func (s *scimStub) delete(w http.ResponseWriter, id string) {
	if s.failDelete {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	delete(s.users, id)
	delete(s.groups, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *scimStub) decode(w http.ResponseWriter, r *http.Request, into any) bool {
	body, err := readAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	if err := json.Unmarshal([]byte(body), into); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func (s *scimStub) reply(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", scimContentType)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

// pathID returns the trailing path segment.
func pathID(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// newSyncStack wires a real store (Postgres fixture) with the fake
// source, and registers one provider bound to the stub server.
func newSyncStack(t *testing.T, source *fakeSource, stub *scimStub) (*Service, ServiceProvider) {
	t.Helper()
	store, _ := newStore(t)

	created, err := store.Create(t.Context(), UpsertParams{
		Endpoint:     stub.srv.URL,
		Token:        "sync-token",
		OIDCClientID: clientFixtureID,
	})
	require.NoError(t, err)

	// Decrypt to the plaintext view the service expects.
	provider, err := store.GetByID(t.Context(), created.ID)
	require.NoError(t, err)

	svc := NewService(store, source, logger.Slog(logger.NewMock()))
	return svc, provider
}

func userRow(id, username string) ScimUserRow {
	return ScimUserRow{ID: id, Username: username, DisplayName: username, Active: true, Email: username + "@tango.test"}
}

func TestSyncProviderCreatesUpdatesDeletes(t *testing.T) {
	ctx := t.Context()
	stub := newSCIMStub(t)
	source := &fakeSource{
		users:  []ScimUserRow{userRow("u1", "ada_wong")},
		groups: []ScimGroupRow{{ID: "g1", Name: "Devs", Members: []string{"u1"}}},
	}
	svc, provider := newSyncStack(t, source, stub)

	// First sync: both snapshot entries are missing remotely → create.
	require.NoError(t, svc.SyncProvider(ctx, provider))
	assert.Len(t, stub.users, 1)
	assert.Len(t, stub.groups, 1)
	assert.Contains(t, stub.requests, "POST /Users")
	assert.Contains(t, stub.requests, "POST /Groups")

	// The bearer token rides on every request.
	for _, got := range stub.bearers {
		assert.Equal(t, "Bearer sync-token", got)
	}

	// The created group references the *remote* user id, not u1.
	var group ScimGroup
	for _, g := range stub.groups {
		group = g
	}
	require.Len(t, group.Members, 1)
	assert.Equal(t, stubRemoteUserID(t, stub), group.Members[0].Value)

	// Second sync with an unchanged snapshot: updates only.
	stub.requests = nil
	require.NoError(t, svc.SyncProvider(ctx, provider))
	remoteUserID := stubRemoteUserID(t, stub)
	remoteGroupID := stubRemoteGroupID(t, stub)
	assert.Contains(t, stub.requests, "PUT /Users/"+remoteUserID)
	assert.Contains(t, stub.requests, "PUT /Groups/"+remoteGroupID)
	assert.NotContains(t, stub.requests, "POST /Users")

	// Snapshot drained: the remote copies are deleted.
	source.users, source.groups = nil, nil
	stub.requests = nil
	require.NoError(t, svc.SyncProvider(ctx, provider))
	assert.Contains(t, stub.requests, "DELETE /Users/"+remoteUserID)
	assert.Contains(t, stub.requests, "DELETE /Groups/"+remoteGroupID)
	assert.Empty(t, stub.users)
	assert.Empty(t, stub.groups)

	// The provider row carries the sync stamp.
	synced, err := svc.store.GetByID(ctx, provider.ID)
	require.NoError(t, err)
	assert.NotNil(t, synced.LastSyncedAt)
}

// stubRemoteUserID returns the single remote user's id.
func stubRemoteUserID(t *testing.T, stub *scimStub) string {
	t.Helper()
	for id := range stub.users {
		return id
	}
	t.Fatal("no remote user")
	return ""
}

// stubRemoteGroupID returns the single remote group's id.
func stubRemoteGroupID(t *testing.T, stub *scimStub) string {
	t.Helper()
	for id := range stub.groups {
		return id
	}
	t.Fatal("no remote group")
	return ""
}

func TestSyncProviderKeepsRemoteEntriesWithoutExternalID(t *testing.T) {
	stub := newSCIMStub(t)
	svc, provider := newSyncStack(t, &fakeSource{}, stub)

	// A remote entry without externalId is invisible to the diff and
	// must survive every sync.
	stub.users["remote-orph"] = ScimUser{ResourceData: ResourceData{ID: "remote-orph"}}
	require.NoError(t, svc.SyncProvider(t.Context(), provider))
	assert.Equal(t, "remote-orph", stubRemoteUserID(t, stub))
}

func TestSyncProviderListError(t *testing.T) {
	stub := newSCIMStub(t)
	stub.failList = true
	svc, provider := newSyncStack(t, &fakeSource{}, stub)

	err := svc.SyncProvider(t.Context(), provider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list users")
}

func TestSyncProviderDeleteError(t *testing.T) {
	stub := newSCIMStub(t)
	stub.failDelete = true
	svc, provider := newSyncStack(t, &fakeSource{}, stub)

	// A remote user with an externalId but no snapshot counterpart is
	// deleted; the failing stub turns that into an error.
	stub.users["remote-gone"] = ScimUser{
		ResourceData: ResourceData{ID: "remote-gone", ExternalID: "u9"},
		UserName:     "ghost",
	}
	err := svc.SyncProvider(t.Context(), provider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete user u9")
}

func TestSyncProviderSourceError(t *testing.T) {
	stub := newSCIMStub(t)
	svc, provider := newSyncStack(t, &fakeSource{userErr: assert.AnError}, stub)

	err := svc.SyncProvider(t.Context(), provider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot users")
}

func TestSyncProviderRetries429(t *testing.T) {
	stub := newSCIMStub(t)
	stub.rateLimitOne = true // answered with Retry-After: 1 once
	svc, provider := newSyncStack(t, &fakeSource{}, stub)

	require.NoError(t, svc.SyncProvider(t.Context(), provider))
	// The request that hit the 429 was retried to success.
	lists := 0
	for _, r := range stub.requests {
		if r == "GET /Users" {
			lists++
		}
	}
	assert.Equal(t, 2, lists, "the rate-limited list must be retried")
}

func TestSyncAllPushesEveryProviderAndJoinsErrors(t *testing.T) {
	ctx := t.Context()
	stub := newSCIMStub(t)
	source := &fakeSource{users: []ScimUserRow{userRow("u1", "ada_wong")}}
	svc, _ := newSyncStack(t, source, stub)

	// The container DB is shared across the test binary: drop rows
	// earlier tests left behind, they point at closed stub servers.
	if _, err := fixtureDS.Exec(ctx, "DELETE FROM public.scim_service_providers"); err != nil {
		t.Fatalf("clean providers: %v", err)
	}

	stubProvider, err := svc.store.Create(ctx, UpsertParams{
		Endpoint: stub.srv.URL, Token: "sync-all", OIDCClientID: clientFixtureID,
	})
	require.NoError(t, err)
	require.NoError(t, svc.SyncAll(ctx))
	assert.Len(t, stub.users, 1)

	// A second provider bound to a dead endpoint fails the join but
	// must not abort the loop.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	second := newClientFixture(t, fixtureDS, "scim-second")
	_, err = svc.store.Create(ctx, UpsertParams{
		Endpoint: deadURL, Token: "t", OIDCClientID: second,
	})
	require.NoError(t, err)
	assert.Error(t, svc.SyncAll(ctx))

	// The stub provider still synced (the loop kept going).
	synced, err := svc.store.GetByID(ctx, stubProvider.ID)
	require.NoError(t, err)
	assert.NotNil(t, synced.LastSyncedAt)
}

func TestRetryDelay(t *testing.T) {
	assert.Equal(t, 7*time.Second, retryDelay("7", 1))
	assert.Equal(t, 2*time.Second, retryDelay("", 1), "falls back to attempt backoff")
	assert.Equal(t, 4*time.Second, retryDelay("bogus", 2))
	assert.Equal(t, 2*time.Second, retryDelay("0", 1), "non-positive Retry-After falls back")
}

func TestUpsertParamsValidate(t *testing.T) {
	base := UpsertParams{Endpoint: "https://scim.example.com", Token: "t", OIDCClientID: "c"}
	assert.NoError(t, base.Validate())

	noScheme := base
	noScheme.Endpoint = "ftp://scim.example.com"
	assert.Error(t, noScheme.Validate())

	relative := base
	relative.Endpoint = "/scim"
	assert.Error(t, relative.Validate())

	noClient := base
	noClient.OIDCClientID = "  "
	assert.Error(t, noClient.Validate())
}

func TestResourceAccessors(t *testing.T) {
	var nilMeta ScimUser
	assert.Empty(t, nilMeta.GetMeta(), "missing meta decodes to the zero value")

	res := ResourceData{ID: "id1", ExternalID: "ext1", Schemas: []string{"s"},
		Meta: &ResourceMeta{ResourceType: "User"}}
	assert.Equal(t, "id1", res.GetID())
	assert.Equal(t, "ext1", res.GetExternalID())
	assert.Equal(t, []string{"s"}, res.GetSchemas())
	assert.Equal(t, "User", res.GetMeta().ResourceType)

	assert.Equal(t, "ext2", ScimUser{ResourceData: ResourceData{ExternalID: "ext2"}}.GetExternalID())
	assert.Equal(t, "ext3", ScimGroup{ResourceData: ResourceData{ExternalID: "ext3"}}.GetExternalID())
}

func TestFindByExternalID(t *testing.T) {
	resources := []ScimUser{
		{ResourceData: ResourceData{ID: "a", ExternalID: "u1"}},
		{ResourceData: ResourceData{ID: "b", ExternalID: "u2"}},
	}
	assert.Equal(t, "b", findByExternalID(resources, "u2").ID)
	assert.Nil(t, findByExternalID(resources, "missing"))
	assert.Nil(t, findByExternalID([]ScimUser{}, "u1"))
}

func TestPayloadMappers(t *testing.T) {
	payload := scimUserPayload(ScimUserRow{
		ID: "u1", Username: "ada_wong", DisplayName: "Ada", FirstName: "Ada", LastName: "Wong",
		Email: "ada@tango.test", Active: true,
	})
	assert.Equal(t, "ada_wong", payload.UserName)
	assert.Equal(t, "u1", payload.ExternalID)
	assert.True(t, payload.Active)
	require.Len(t, payload.Emails, 1)
	assert.Equal(t, "ada@tango.test", payload.Emails[0].Value)
	assert.True(t, payload.Emails[0].Primary)

	noEmail := scimUserPayload(ScimUserRow{ID: "u2", Username: "bob", Active: true})
	assert.Empty(t, noEmail.Emails)

	group := scimGroupPayload(ScimGroupRow{ID: "g1", Name: "Devs", Members: []string{"u1", "u9"}},
		map[string]string{"u1": "remote-1"})
	assert.Equal(t, "Devs", group.Display)
	require.Len(t, group.Members, 1, "members without a remote id are dropped")
	assert.Equal(t, "remote-1", group.Members[0].Value)

	empty := scimGroupPayload(ScimGroupRow{ID: "g2", Name: "Empty"}, nil)
	assert.NotNil(t, empty.Members, "members stay non-nil for SCIM list output")
	assert.Empty(t, empty.Members)
}
