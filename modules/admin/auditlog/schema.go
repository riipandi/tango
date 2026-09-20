// Schema contracts and domain types of the auditlog module.
package auditlog

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ua-parser/uap-go/uaparser"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// AuditLogID identifies an audit_logs row: a UUIDv7 suffix plus a
// snake_case prefix matching the singular table name.
type AuditLogID = typeid.TypeID[auditLogPrefix]

type auditLogPrefix struct{}

func (auditLogPrefix) Prefix() string { return "audit_log" }

// Trigger classifies who emitted the event (audit_event_trigger enum).
type Trigger string

// Trigger values mirroring the database enum.
const (
	TriggerUser     Trigger = "user"
	TriggerSystem   Trigger = "system"
	TriggerExternal Trigger = "external"
)

// Status is the action outcome (audit_action_status enum).
type Status string

// Status values mirroring the database enum.
const (
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusPending Status = "pending"
	StatusUnknown Status = "unknown"
)

// Entry is a single audit record. UserID and ResourceID are raw UUID
// strings (columns), so the module stays decoupled from other
// domains' typed IDs; adapters convert at the boundary.
type Entry struct {
	ID      AuditLogID `json:"id"`
	Event   string     `json:"event"`
	Trigger Trigger    `json:"trigger"`
	Status  Status     `json:"status"`

	Payload map[string]any `json:"payload,omitzero"`

	ResourceType string  `json:"resource_type,omitzero"`
	ResourceID   *string `json:"resource_id,omitzero"`
	UserID       *string `json:"user_id,omitzero"`
	IPAddress    *string `json:"ip_address,omitzero"`
	UserAgent    *string `json:"user_agent,omitzero"`

	// Device is the human-readable client summary parsed from the
	// user agent at read time ("<browser> on <os> <version>").
	Device string `json:"device,omitzero"`

	CreatedAt time.Time `json:"created_at"`
}

// uaParser compiles the UA regexes once; every entry render reuses it.
var uaParser = sync.OnceValue(func() *uaparser.Parser {
	return uaparser.NewFromSaved()
})

// deviceFromUserAgent renders the compact device summary ("<agent
// family> on <os family> <version>"); unknown agents degrade to
// their family names, empty agents to an empty summary.
func deviceFromUserAgent(userAgent *string) string {
	if userAgent == nil || *userAgent == "" {
		return ""
	}
	ua := uaParser().Parse(*userAgent)
	osVersion := strings.Trim(strings.Join([]string{ua.Os.Major, ua.Os.Minor, ua.Os.Patch}, "."), ".")
	osVersion = strings.TrimSuffix(osVersion, "..")

	name := strings.TrimPrefix(ua.UserAgent.Family, "Other")
	os := strings.TrimPrefix(ua.Os.Family, "Other")
	switch {
	case name == "" && os == "":
		return ""
	case os == "":
		return name
	case name == "":
		return os + " " + osVersion
	default:
		return name + " on " + os + " " + osVersion
	}
}

// ListFilters narrows the listing. UserID is a raw UUID string
// (columns stay decoupled from other domains' typed IDs).
type ListFilters struct {
	UserID string
	Event  string
	From   *time.Time
	To     *time.Time
}

// Page is the store-level paging window: plain ints with no HTTP
// dependency. The handler converts the request query into it.
type Page struct {
	Page  int
	Limit int
}

// All reports whether the listing skips paging (page or limit is the
// all marker -1).
func (p Page) All() bool { return p.Page == -1 || p.Limit == -1 }

// Offset returns the SQL offset for the current page.
func (p Page) Offset() int {
	if p.All() || p.Page < 1 || p.Limit < 1 {
		return 0
	}
	return (p.Page - 1) * p.Limit
}

// Store abstracts audit persistence: Postgres for production, no
// memory store. Record assigns Entry.ID (and CreatedAt when zero) on
// the pointed-to entry. List returns the matching entries (newest
// first) plus the total row count.
type Store interface {
	// Record writes one entry. A non-nil exec joins the caller's
	// transaction so the audit row commits (or rolls back) with the
	// domain write; nil writes standalone.
	Record(ctx context.Context, entry *Entry, exec ...datastore.Executor) error
	List(ctx context.Context, filters ListFilters, params Page) ([]Entry, int, error)
	UserFilterValues(ctx context.Context) ([]string, error)
	ClientNameFilterValues(ctx context.Context) ([]string, error)
}
