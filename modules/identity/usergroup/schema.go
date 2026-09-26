package usergroup

import (
	"time"

	"uuid"
)

// GroupTable is the user groups table. The migrations own the schema; this
// constant is how Go code names it, so a table rename touches one line.
const GroupTable = "public.user_groups"

// GroupMemberTable is the junction that makes an account a member of a group.
// Both foreign keys cascade, so a group's removal takes its membership rows
// with it and an account's removal takes its memberships.
const GroupMemberTable = "public.user_groups_users"

// ResourceGroup is the resource type an audit record names when the change is
// about a group. The record's user_id stays empty — a group is not an account
// — and the group is named in resource_type and resource_id, the way any other
// acted-on resource is.
const ResourceGroup = "user_group"

// GroupSchema is one row of GroupTable. The updated column is nullable by
// construction: the trigger fills it on the first update, so a row never
// updated reads as nil, not as a zero instant.
type GroupSchema struct {
	ID          uuid.UUID  `db:"id"`
	Name        string     `db:"name"`
	DisplayName string     `db:"display_name"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   *time.Time `db:"updated_at"`
}

// GroupRow is one row of a list answer: the group plus the member count the
// query computes. The count is not a column — it is what the LEFT JOIN counts,
// and a group with no members reads as zero because the join is left.
type GroupRow struct {
	GroupSchema
	UserCount int
}
