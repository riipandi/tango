// Schema contracts and domain types of the auditlog module.
package auditlog

import "time"

// Event is a single audit entry. Other modules record events through
// the module's public API instead of touching storage directly.
type Event struct {
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Target    string    `json:"target"`
	Timestamp time.Time `json:"timestamp"`
}
