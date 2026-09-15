package ldapsync

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"
	"golang.org/x/text/unicode/norm"

	"github.com/riipandi/tango/internal/datastore"
)

// datastoreExecutor keeps the store dependency decoupled in the service.
type datastoreExecutor = datastore.Executor

// SyncInterval is the recurring sync cadence.
const SyncInterval = time.Hour

// ldapClient is the subset of the go-ldap client the sync uses; tests
// supply a fake.
type ldapClient interface {
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	Bind(username, password string) error
	Close() error
}

// dialer builds an LDAP client from settings; overridable in tests.
type dialer func(settings LDAPSettings) (ldapClient, error)

// Service performs the reconciliation. It stays free of scheduling
// concerns: the caller decides when a sync runs (ticker or endpoint).
type Service struct {
	users  *Reconciler
	dial   dialer
	http   *http.Client
	logger *slog.Logger
}

// NewService builds the sync service over the transactional store.
func NewService(exec datastoreExecutor, log *slog.Logger) *Service {
	s := &Service{
		users:  NewReconciler(exec),
		http:   &http.Client{Timeout: 15 * time.Second},
		logger: log,
	}
	s.dial = s.dialReal
	return s
}

// WithDialer replaces the client factory (tests).
func (s *Service) WithDialer(d dialer) *Service {
	s.dial = d
	return s
}

func (s *Service) dialReal(settings LDAPSettings) (ldapClient, error) {
	client, err := ldap.DialURL(settings.URL, ldap.DialWithTLSConfig(&tls.Config{
		InsecureSkipVerify: settings.SkipCertVerify, //nolint:gosec // admin opt-in
	}))
	if err != nil {
		return nil, fmt.Errorf("ldapsync: connect: %w", err)
	}
	if err := client.Bind(settings.BindDN, settings.BindPassword); err != nil {
		client.Close()
		return nil, fmt.Errorf("ldapsync: bind: %w", err)
	}
	return client, nil
}

// desiredState is the LDAP snapshot.
type desiredState struct {
	users      []desiredUser
	groups     []desiredGroup
	adminNames map[string]struct{}
}

// SyncAll runs one full reconciliation against the directory.
func (s *Service) SyncAll(ctx context.Context, settings LDAPSettings) (SyncStats, error) {
	settings = settings.withDefaults()
	if !settings.Enabled {
		return SyncStats{}, ErrDisabled
	}
	if settings.URL == "" || settings.Base == "" {
		return SyncStats{}, ErrNotConfigured
	}

	client, err := s.dial(settings)
	if err != nil {
		return SyncStats{}, err
	}
	defer client.Close()

	state, err := s.fetchDesiredState(ctx, client, settings)
	if err != nil {
		return SyncStats{}, fmt.Errorf("ldapsync: fetch state: %w", err)
	}

	// Admin flag comes from the configured group, resolved after the
	// member snapshot is complete.
	for i := range state.users {
		if _, isAdmin := state.adminNames[state.users[i].Username]; isAdmin {
			state.users[i].IsAdmin = true
		}
	}

	stats, err := s.users.Run(ctx, state.users, state.groups, settings.SoftDeleteUsers)
	if err != nil {
		return stats, err
	}
	s.logger.InfoContext(ctx, "LDAP sync completed",
		slog.Int("users_created", stats.UsersCreated),
		slog.Int("users_updated", stats.UsersUpdated),
		slog.Int("users_disabled", stats.UsersDisabled),
		slog.Int("users_deleted", stats.UsersDeleted),
		slog.Int("groups_created", stats.GroupsCreated),
		slog.Int("groups_updated", stats.GroupsUpdated),
		slog.Int("groups_deleted", stats.GroupsDeleted),
	)
	return stats, nil
}

func (s *Service) fetchDesiredState(ctx context.Context, client ldapClient, settings LDAPSettings) (desiredState, error) {
	state := desiredState{adminNames: map[string]struct{}{}}

	users, usernamesByDN, err := s.fetchUsers(ctx, client, settings)
	if err != nil {
		return state, err
	}
	state.users = users

	groups, err := s.fetchGroups(ctx, client, settings, usernamesByDN)
	if err != nil {
		return state, err
	}
	state.groups = groups.groups

	for _, name := range groups.adminMembers {
		state.adminNames[name] = struct{}{}
	}
	return state, nil
}

// fetchUsers pulls the user subtree and builds the DN→username cache.
func (s *Service) fetchUsers(ctx context.Context, client ldapClient, settings LDAPSettings) ([]desiredUser, map[string]string, error) {
	search := ldap.NewSearchRequest(
		settings.Base,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases, 0, 0, false,
		settings.UserFilter,
		[]string{
			settings.AttrUserUniqueID,
			settings.AttrUserUsername,
			settings.AttrUserEmail,
			settings.AttrUserFirstName,
			settings.AttrUserLastName,
			settings.AttrUserDisplay,
		},
		nil,
	)
	result, err := client.Search(search)
	if err != nil {
		return nil, nil, fmt.Errorf("ldapsync: search users: %w", err)
	}

	usernamesByDN := make(map[string]string, len(result.Entries))
	desired := make([]desiredUser, 0, len(result.Entries))
	for _, entry := range result.Entries {
		username := norm.NFC.String(entry.GetAttributeValue(settings.AttrUserUsername))
		if dn := normalizeDN(entry.DN); dn != "" && username != "" {
			usernamesByDN[dn] = username
		}

		ldapID := entry.GetAttributeValue(settings.AttrUserUniqueID)
		if strings.TrimSpace(ldapID) == "" {
			s.logger.WarnContext(ctx, "skipping LDAP user without a unique identifier",
				slog.String("attribute", settings.AttrUserUniqueID))
			continue
		}

		du := desiredUser{
			LDAPID:      ldapID,
			Username:    sanitizeUsername(username),
			Email:       entry.GetAttributeValue(settings.AttrUserEmail),
			FirstName:   entry.GetAttributeValue(settings.AttrUserFirstName),
			LastName:    entry.GetAttributeValue(settings.AttrUserLastName),
			DisplayName: entry.GetAttributeValue(settings.AttrUserDisplay),
		}
		if du.DisplayName == "" {
			du.DisplayName = strings.TrimSpace(du.FirstName + " " + du.LastName)
		}
		if !utf8.ValidString(du.Username) || du.Username == "" {
			s.logger.WarnContext(ctx, "skipping LDAP user without a usable username", slog.String("dn", entry.DN))
			continue
		}
		desired = append(desired, du)
	}
	return desired, usernamesByDN, nil
}

type fetchedGroups struct {
	groups       []desiredGroup
	adminMembers []string
}

// fetchGroups pulls the group subtree, resolving member values to
// usernames via the DN cache, direct DN property extraction, bare
// uid values (posixGroup/memberUid), or a base-object lookup.
func (s *Service) fetchGroups(ctx context.Context, client ldapClient, settings LDAPSettings, usernamesByDN map[string]string) (fetchedGroups, error) {
	search := ldap.NewSearchRequest(
		settings.Base,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases, 0, 0, false,
		settings.GroupFilter,
		[]string{settings.AttrGroupUniqueID, settings.AttrGroupName, settings.AttrGroupMember},
		nil,
	)
	result, err := client.Search(search)
	if err != nil {
		return fetchedGroups{}, fmt.Errorf("ldapsync: search groups: %w", err)
	}

	out := fetchedGroups{groups: make([]desiredGroup, 0, len(result.Entries))}
	for _, entry := range result.Entries {
		ldapID := entry.GetAttributeValue(settings.AttrGroupUniqueID)
		if strings.TrimSpace(ldapID) == "" {
			s.logger.WarnContext(ctx, "skipping LDAP group without a unique identifier",
				slog.String("attribute", settings.AttrGroupUniqueID))
			continue
		}

		name := entry.GetAttributeValue(settings.AttrGroupName)
		if name == "" {
			continue
		}

		dg := desiredGroup{LDAPID: ldapID, Name: name}
		for _, member := range entry.GetAttributeValues(settings.AttrGroupMember) {
			username := s.resolveMember(ctx, client, member, usernamesByDN, settings.AttrUserUsername)
			if username != "" {
				dg.Members = append(dg.Members, username)
			}
		}
		out.groups = append(out.groups, dg)

		if settings.AdminGroupName != "" && strings.EqualFold(name, settings.AdminGroupName) {
			out.adminMembers = append(out.adminMembers, dg.Members...)
		}
	}
	return out, nil
}

// resolveMember maps a group member attribute value to a username.
func (s *Service) resolveMember(ctx context.Context, client ldapClient, member string, usernamesByDN map[string]string, usernameAttr string) string {
	if username, ok := usernamesByDN[normalizeDN(member)]; ok && username != "" {
		return username
	}
	// Not a DN (e.g. memberUid holds a bare username).
	if _, err := ldap.ParseDN(member); err != nil {
		return norm.NFC.String(member)
	}
	if username := dnProperty(usernameAttr, member); username != "" {
		return norm.NFC.String(username)
	}
	// Last resort: fetch the referenced entry.
	search := ldap.NewSearchRequest(member, ldap.ScopeBaseObject,
		ldap.NeverDerefAliases, 0, 0, false, "(objectClass=*)",
		[]string{usernameAttr}, nil)
	result, err := client.Search(search)
	if err != nil || len(result.Entries) == 0 {
		s.logger.WarnContext(ctx, "could not resolve LDAP group member", slog.String("member", member))
		return ""
	}
	return norm.NFC.String(result.Entries[0].GetAttributeValue(usernameAttr))
}

// normalizeDN lowercases and canonicalizes an LDAP DN for map keys.
func normalizeDN(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(dn))
	}
	var sb strings.Builder
	for i, rdn := range parsed.RDNs {
		if i > 0 {
			sb.WriteString(",")
		}
		for _, attr := range rdn.Attributes {
			sb.WriteString(strings.ToLower(attr.Type))
			sb.WriteString("=")
			sb.WriteString(attr.Value)
		}
	}
	return sb.String()
}

// sanitizeUsername maps an LDAP username onto tango's username rules
// (^[a-zA-Z0-9_]{3,32}$): separators become underscores, everything
// else invalid is dropped.
func sanitizeUsername(raw string) string {
	replaced := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		default:
			return '_'
		}
	}, strings.TrimSpace(raw))
	return strings.ToLower(replaced)
}

// dnProperty extracts one attribute value from a DN string.
func dnProperty(property, dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil {
		return ""
	}
	for _, rdn := range parsed.RDNs {
		for _, attr := range rdn.Attributes {
			if strings.EqualFold(attr.Type, property) {
				return attr.Value
			}
		}
	}
	return ""
}

// FetchPicture downloads an LDAP profile picture (URL) for storage;
// unused while profile pictures are not part of the sync.
func (s *Service) FetchPicture(ctx context.Context, raw string) ([]byte, error) {
	if _, err := url.ParseRequestURI(raw); err != nil {
		return nil, fmt.Errorf("ldapsync: picture is not a URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ldapsync: download picture: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ldapsync: picture download status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
