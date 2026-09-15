package apiaccess

import (
	"context"
	"errors"
	"fmt"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// Table constants: the API registry plus grant junctions.
const (
	apisTable        = "public.apis"
	permissionsTable = "public.api_permissions"
	grantsTable      = "public.oidc_client_api_grants"
	grantPermsTable  = "public.oidc_client_api_grant_permissions"
	clientsTable     = "public.oidc_clients"
)

// Grant subjects on the permission junction. CIMD access is not
// stored per client: it is computed from the API flag plus the
// permission allowlist.
const (
	subjectClient        = "client"
	subjectUserDelegated = "user_delegated"
)

// PostgresStore persists APIs in public.apis. List-replace writes
// run inside a WithTx transaction.
type PostgresStore struct {
	store datastore.Store
	exec  datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production API store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{store: store, exec: store}
}

// apiColumns is the SELECT list; keep order in sync with scanAPI.
var apiColumns = []string{"id", "name", "resource", "allow_cimd_clients", "created_at", "updated_at"}

// Create inserts an API; uuidv7() fills the ID.
func (s *PostgresStore) Create(ctx context.Context, params CreateParams) (API, error) {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(apisTable)
	ib.Cols("name", "resource")
	ib.Values(params.Name, params.Resource)
	ib.Returning(apiColumns...)

	query, args := ib.Build()
	a, err := scanAPI(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return API{}, mapErr(err)
	}
	return a, nil
}

// GetByID resolves one API with its permissions.
func (s *PostgresStore) GetByID(ctx context.Context, id APIID) (API, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(apiColumns...)
	sb.From(apisTable)
	sb.Where(sb.E("id", id.UUIDBytes()))

	query, args := sb.Build()
	a, err := scanAPI(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return API{}, mapErr(err)
	}

	a.Permissions, err = s.permissionsFor(ctx, id)
	if err != nil {
		return API{}, err
	}
	return a, nil
}

// Update patches the name and returns the fresh row.
func (s *PostgresStore) Update(ctx context.Context, id APIID, params UpdateParams) (API, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(apisTable)
	ub.Set(ub.Assign("name", params.Name))
	ub.Where(ub.E("id", id.UUIDBytes()))
	ub.Returning(apiColumns...)

	query, args := ub.Build()
	a, err := scanAPI(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return API{}, mapErr(err)
	}

	a.Permissions, err = s.permissionsFor(ctx, id)
	if err != nil {
		return API{}, err
	}
	return a, nil
}

// Delete removes the API; permissions and grants cascade.
func (s *PostgresStore) Delete(ctx context.Context, id APIID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(apisTable)
	db.Where(db.E("id", id.UUIDBytes()))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("apiaccess store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// List returns matching APIs newest first plus the total count.
// Permissions are not hydrated here (list views do not need them).
func (s *PostgresStore) List(ctx context.Context, params ListParams) ([]API, int, error) {
	csb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	csb.Select("count(*)")
	csb.From(apisTable)
	if params.Query != "" {
		pattern := "%" + params.Query + "%"
		csb.Where(csb.Or(csb.Like("name", pattern), csb.Like("resource", pattern)))
	}

	countQuery, countArgs := csb.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("apiaccess store: count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(apiColumns...)
	sb.From(apisTable)
	if params.Query != "" {
		pattern := "%" + params.Query + "%"
		sb.Where(sb.Or(sb.Like("name", pattern), sb.Like("resource", pattern)))
	}
	sb.OrderBy("created_at DESC", "id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("apiaccess store: list: %w", err)
	}
	defer rows.Close()

	out := []API{}
	for rows.Next() {
		a, scanErr := scanAPI(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// SetPermissions atomically replaces the permission set of one API.
func (s *PostgresStore) SetPermissions(ctx context.Context, id APIID, perms []PermissionInput) ([]Permission, error) {
	var out []Permission
	err := s.store.WithTx(ctx, func(tx datastore.Executor) error {
		db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
		db.DeleteFrom(permissionsTable)
		db.Where(db.E("api_id", id.UUIDBytes()))

		delQuery, delArgs := db.Build()
		if _, err := tx.Exec(ctx, delQuery, delArgs...); err != nil {
			return fmt.Errorf("apiaccess store: clear permissions: %w", err)
		}

		for _, perm := range perms {
			ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
			ib.InsertInto(permissionsTable)
			ib.Cols("api_id", "key", "name", "description")
			ib.Values(id.UUIDBytes(), perm.Key, perm.Name, perm.Description)
			ib.Returning("id", "key", "name", "description", "allowed_for_cimd_clients", "created_at")

			query, args := ib.Build()
			p, err := scanPermission(tx.QueryRow(ctx, query, args...))
			if err != nil {
				return mapErr(err)
			}
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// permissionsFor loads the permission rows of one API, ordered by key.
func (s *PostgresStore) permissionsFor(ctx context.Context, id APIID) ([]Permission, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "key", "name", "description", "allowed_for_cimd_clients", "created_at")
	sb.From(permissionsTable)
	sb.Where(sb.E("api_id", id.UUIDBytes()))
	sb.OrderBy("key")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("apiaccess store: permissions: %w", err)
	}
	defer rows.Close()

	out := []Permission{}
	for rows.Next() {
		p, scanErr := scanPermission(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// clientColumns is the apiClientDto projection over oidc_clients.
var clientColumns = []string{"id", "name", "client_type", "is_public", "image_type IS NOT NULL", "dark_image_type IS NOT NULL"}

// ListClientsWithAccess pages the clients holding a grant on the API.
func (s *PostgresStore) ListClientsWithAccess(ctx context.Context, id APIID, params ListParams) ([]ClientRef, int, error) {
	return s.listClients(ctx, id, params, true)
}

// ListAssignableClients pages the clients without a grant yet.
func (s *PostgresStore) ListAssignableClients(ctx context.Context, id APIID, params ListParams) ([]ClientRef, int, error) {
	return s.listClients(ctx, id, params, false)
}

func (s *PostgresStore) listClients(ctx context.Context, id APIID, params ListParams, granted bool) ([]ClientRef, int, error) {
	join := "LEFT JOIN"
	if granted {
		join = "JOIN"
	}
	from := apisTable + " a " + join + " " + grantsTable + " g ON g.api_id = a.id " +
		"JOIN " + clientsTable + " c ON c.id = g.client_id"
	extra := "g.client_id IS NULL"
	if granted {
		extra = "g.client_id IS NOT NULL"
	}

	csb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	csb.Select("count(DISTINCT c.id)")
	csb.From(from)
	csb.Where(csb.E("a.id", id.UUIDBytes()), extra)
	if params.Query != "" {
		csb.Where(csb.Like("c.name", "%"+params.Query+"%"))
	}

	countQuery, countArgs := csb.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("apiaccess store: client count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(qualifiedClientColumns("c")...)
	sb.From(from)
	sb.Where(sb.E("a.id", id.UUIDBytes()), extra)
	if params.Query != "" {
		sb.Where(sb.Like("c.name", "%"+params.Query+"%"))
	}
	sb.OrderBy("c.name")
	sb.GroupBy(qualifiedClientColumns("c")...)
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("apiaccess store: clients: %w", err)
	}
	defer rows.Close()

	out := []ClientRef{}
	for rows.Next() {
		c, scanErr := scanClient(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// GrantFor resolves one grant; ErrNotFound when the pair is absent.
func (s *PostgresStore) GrantFor(ctx context.Context, id APIID, clientID string) (Grant, error) {
	return s.grantRow(ctx, id, clientID)
}

// grantRow loads the grant flags plus the per-subject permission IDs.
func (s *PostgresStore) grantRow(ctx context.Context, id APIID, clientID string) (Grant, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("user_delegated_access", "client_access")
	sb.From(grantsTable)
	sb.Where(sb.E("api_id", id.UUIDBytes()), sb.E("client_id", clientID))

	query, args := sb.Build()
	var g Grant
	g.APIID, g.ClientID = id.String(), clientID
	err := s.exec.QueryRow(ctx, query, args...).Scan(&g.UserDelegatedAccess, &g.ClientAccess)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Grant{}, ErrNotFound
		}
		return Grant{}, fmt.Errorf("apiaccess store: grant: %w", err)
	}

	if g.UserDelegatedPermissionIDs, err = s.grantPermissionIDs(ctx, id, clientID, subjectUserDelegated); err != nil {
		return Grant{}, err
	}
	g.ClientPermissionIDs, err = s.grantPermissionIDs(ctx, id, clientID, subjectClient)
	return g, err
}

// grantPermissionIDs lists the granted permission IDs for one subject.
func (s *PostgresStore) grantPermissionIDs(ctx context.Context, id APIID, clientID, subject string) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("p.id")
	sb.From(grantPermsTable + " gp")
	sb.Join(permissionsTable + " p ON p.id = gp.permission_id")
	sb.Where(sb.E("gp.api_id", id.UUIDBytes()), sb.E("gp.client_id", clientID), sb.E("gp.subject", subject))
	sb.OrderBy("p.key")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("apiaccess store: grant permissions: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var pid string
		if scanErr := rows.Scan(&pid); scanErr != nil {
			continue
		}
		out = append(out, pid)
	}
	return out, rows.Err()
}

// UpsertGrant writes the grant flags and replaces the per-subject
// permission lists atomically. Permission IDs must belong to the API.
func (s *PostgresStore) UpsertGrant(ctx context.Context, id APIID, clientID string, params GrantParams) (Grant, error) {
	err := s.store.WithTx(ctx, func(tx datastore.Executor) error {
		// FK on client_id would surface as a 500; check first for a
		// deterministic domain error.
		cb := sqlbuilder.PostgreSQL.NewSelectBuilder()
		cb.Select("count(*)")
		cb.From(clientsTable)
		cb.Where(cb.E("id", clientID))

		cQuery, cArgs := cb.Build()
		var known int
		if err := tx.QueryRow(ctx, cQuery, cArgs...).Scan(&known); err != nil {
			return fmt.Errorf("apiaccess store: check client: %w", err)
		}
		if known == 0 {
			return ErrUnknownClient
		}

		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(grantsTable)
		ib.Cols("api_id", "client_id", "user_delegated_access", "client_access")
		ib.Values(id.UUIDBytes(), clientID, params.UserDelegatedAccess, params.ClientAccess)
		ib.SQL("ON CONFLICT (api_id, client_id) DO UPDATE SET")
		ib.SQL("user_delegated_access = EXCLUDED.user_delegated_access, client_access = EXCLUDED.client_access, updated_at = now()")

		query, args := ib.Build()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("apiaccess store: upsert grant: %w", err)
		}

		db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
		db.DeleteFrom(grantPermsTable)
		db.Where(db.E("api_id", id.UUIDBytes()), db.E("client_id", clientID))

		delQuery, delArgs := db.Build()
		if _, err := tx.Exec(ctx, delQuery, delArgs...); err != nil {
			return fmt.Errorf("apiaccess store: clear grant permissions: %w", err)
		}

		type subjectPerms struct {
			ids     []string
			subject string
		}
		for _, row := range []subjectPerms{
			{ids: params.UserDelegatedPermissionIDs, subject: subjectUserDelegated},
			{ids: params.ClientPermissionIDs, subject: subjectClient},
		} {
			if len(row.ids) == 0 {
				continue
			}
			// Verify the whole batch belongs to this API before
			// inserting; otherwise the FK would 500 later.
			vb := sqlbuilder.PostgreSQL.NewSelectBuilder()
			vb.Select("count(DISTINCT id)")
			vb.From(permissionsTable)
			rawIDs := make([]any, 0, len(row.ids))
			for _, pid := range row.ids {
				rawIDs = append(rawIDs, pid)
			}
			vb.Where(vb.E("api_id", id.UUIDBytes()), vb.In("id", rawIDs...))

			vQuery, vArgs := vb.Build()
			var matched int
			if err := tx.QueryRow(ctx, vQuery, vArgs...).Scan(&matched); err != nil {
				return fmt.Errorf("apiaccess store: check permissions: %w", err)
			}
			if matched != len(row.ids) {
				return ErrUnknownPerms
			}

			ins := sqlbuilder.PostgreSQL.NewInsertBuilder()
			ins.InsertInto(grantPermsTable)
			ins.Cols("api_id", "client_id", "permission_id", "subject")
			for _, pid := range row.ids {
				ins.Values(id.UUIDBytes(), clientID, pid, row.subject)
			}

			iQuery, iArgs := ins.Build()
			if _, err := tx.Exec(ctx, iQuery, iArgs...); err != nil {
				return fmt.Errorf("apiaccess store: add grant permissions: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Grant{}, err
	}
	return s.grantRow(ctx, id, clientID)
}

// DeleteGrant removes one client's grant on the API.
func (s *PostgresStore) DeleteGrant(ctx context.Context, id APIID, clientID string) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(grantsTable)
	db.Where(db.E("api_id", id.UUIDBytes()), db.E("client_id", clientID))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("apiaccess store: delete grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGrantsForClient lists every grant held by one client.
func (s *PostgresStore) ListGrantsForClient(ctx context.Context, clientID string) ([]Grant, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("api_id", "user_delegated_access", "client_access")
	sb.From(grantsTable)
	sb.Where(sb.E("client_id", clientID))
	sb.OrderBy("api_id")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("apiaccess store: grants for client: %w", err)
	}
	defer rows.Close()

	out := []Grant{}
	for rows.Next() {
		var apiUUID string
		var g Grant
		if scanErr := rows.Scan(&apiUUID, &g.UserDelegatedAccess, &g.ClientAccess); scanErr != nil {
			continue
		}
		g.ClientID = clientID
		g.APIID = mustAPIID(apiUUID).String()

		if g.UserDelegatedPermissionIDs, err = s.grantPermissionIDsByUUID(ctx, apiUUID, clientID, subjectUserDelegated); err != nil {
			return nil, err
		}
		if g.ClientPermissionIDs, err = s.grantPermissionIDsByUUID(ctx, apiUUID, clientID, subjectClient); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// grantPermissionIDsByUUID is grantPermissionIDs keyed by the raw
// UUID (used where the typed APIID is not available).
func (s *PostgresStore) grantPermissionIDsByUUID(ctx context.Context, apiUUID, clientID, subject string) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("p.id")
	sb.From(grantPermsTable + " gp")
	sb.Join(permissionsTable + " p ON p.id = gp.permission_id")
	sb.Where(sb.E("gp.api_id", apiUUID), sb.E("gp.client_id", clientID), sb.E("gp.subject", subject))
	sb.OrderBy("p.key")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("apiaccess store: grant permissions: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var pid string
		if scanErr := rows.Scan(&pid); scanErr != nil {
			continue
		}
		out = append(out, pid)
	}
	return out, rows.Err()
}

// ListAssignableAPIs pages the APIs the client holds no grant on.
func (s *PostgresStore) ListAssignableAPIs(ctx context.Context, clientID string, params ListParams) ([]API, int, error) {
	// Collect the granted API UUIDs first, then exclude them with a
	// parameterized NOT IN (no raw string interpolation).
	gb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	gb.Select("api_id")
	gb.From(grantsTable)
	gb.Where(gb.E("client_id", clientID))

	gQuery, gArgs := gb.Build()
	gRows, err := s.exec.Query(ctx, gQuery, gArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("apiaccess store: granted ids: %w", err)
	}
	defer gRows.Close()

	granted := []any{}
	for gRows.Next() {
		var apiUUID string
		if scanErr := gRows.Scan(&apiUUID); scanErr != nil {
			continue
		}
		granted = append(granted, apiUUID)
	}
	if rowsErr := gRows.Err(); rowsErr != nil {
		return nil, 0, fmt.Errorf("apiaccess store: granted ids: %w", rowsErr)
	}

	where := func(sb *sqlbuilder.SelectBuilder) {
		if len(granted) > 0 {
			sb.Where(sb.NotIn("a.id", granted...))
		}
		if params.Query != "" {
			pattern := "%" + params.Query + "%"
			sb.Where(sb.Or(sb.Like("a.name", pattern), sb.Like("a.resource", pattern)))
		}
	}

	csb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	csb.Select("count(*)")
	csb.From(apisTable + " a")
	where(csb)

	countQuery, countArgs := csb.Build()
	var total int
	if countErr := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); countErr != nil {
		return nil, 0, fmt.Errorf("apiaccess store: assignable count: %w", countErr)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(prefixedAPIColumns("a")...)
	sb.From(apisTable + " a")
	where(sb)
	sb.OrderBy("a.name")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("apiaccess store: assignable: %w", err)
	}
	defer rows.Close()

	out := []API{}
	for rows.Next() {
		a, scanErr := scanAPI(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// SetCIMDAccess toggles the API-level flag and replaces the CIMD
// permission allowlist atomically: listed permissions become CIMD
// accessible, all others lose the flag.
func (s *PostgresStore) SetCIMDAccess(ctx context.Context, id APIID, enabled bool, permissionIDs []string) (API, error) {
	err := s.store.WithTx(ctx, func(tx datastore.Executor) error {
		ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
		ub.Update(apisTable)
		ub.Set(ub.Assign("allow_cimd_clients", enabled))
		ub.Where(ub.E("id", id.UUIDBytes()))

		query, args := ub.Build()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("apiaccess store: set cimd flag: %w", err)
		}

		pb := sqlbuilder.PostgreSQL.NewUpdateBuilder()
		pb.Update(permissionsTable)
		pb.Set(pb.Assign("allowed_for_cimd_clients", false))
		pb.Where(pb.E("api_id", id.UUIDBytes()))

		pQuery, pArgs := pb.Build()
		if _, err := tx.Exec(ctx, pQuery, pArgs...); err != nil {
			return fmt.Errorf("apiaccess store: clear cimd permissions: %w", err)
		}
		if !enabled || len(permissionIDs) == 0 {
			return nil
		}

		rawIDs := make([]any, 0, len(permissionIDs))
		for _, pid := range permissionIDs {
			rawIDs = append(rawIDs, pid)
		}
		ub2 := sqlbuilder.PostgreSQL.NewUpdateBuilder()
		ub2.Update(permissionsTable)
		ub2.Set(ub2.Assign("allowed_for_cimd_clients", true))
		ub2.Where(ub2.E("api_id", id.UUIDBytes()), ub2.In("id", rawIDs...))

		query, args = ub2.Build()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("apiaccess store: set cimd permissions: %w", err)
		}
		return nil
	})
	if err != nil {
		return API{}, err
	}
	return s.GetByID(ctx, id)
}

// AllowedScopesForAudience resolves an RFC 8707 resource to the
// permission keys one client may use on it. hasAccess is true when
// the grant row exists with the matching access flag, even with an
// empty permission list.
func (s *PostgresStore) AllowedScopesForAudience(ctx context.Context, clientID, resource string, subjectType string) ([]string, bool, bool, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("a.id", "g.user_delegated_access", "g.client_access")
	sb.From(apisTable + " a")
	sb.Join(grantsTable + " g ON g.api_id = a.id AND g.client_id = " + sb.Var(clientID))
	sb.Where(sb.E("a.resource", resource))

	query, args := sb.Build()
	var (
		apiUUID    string
		userAccess bool
		clientAcc  bool
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(&apiUUID, &userAccess, &clientAcc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, false, nil
		}
		return nil, false, false, fmt.Errorf("apiaccess store: audience lookup: %w", err)
	}

	access := userAccess
	if subjectType == string(SubjectClient) {
		access = clientAcc
	}
	if !access {
		return []string{}, true, false, nil
	}

	subject := subjectUserDelegated
	if subjectType == string(SubjectClient) {
		subject = subjectClient
	}
	scopes, err := s.grantPermissionIDsByUUID(ctx, apiUUID, clientID, subject)
	if err != nil {
		return nil, false, false, err
	}
	return scopes, true, true, nil
}

// prefixedAPIColumns qualifies apiColumns with a table alias.
func prefixedAPIColumns(alias string) []string {
	out := make([]string, 0, len(apiColumns))
	for _, col := range apiColumns {
		out = append(out, alias+"."+col)
	}
	return out
}

// qualifiedClientColumns qualifies clientColumns with an alias,
// keeping the IS NOT NULL projections intact.
func qualifiedClientColumns(alias string) []string {
	out := make([]string, 0, len(clientColumns))
	for _, col := range clientColumns {
		if col == "id" || col == "name" || col == "client_type" || col == "is_public" {
			out = append(out, alias+"."+col)
			continue
		}
		out = append(out, col)
	}
	return out
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanAPI scans one row; keep order in sync with apiColumns.
func scanAPI(row scanner) (API, error) {
	var (
		id        string
		a         API
		updatedAt pgtype.Timestamptz
		createdAt pgtype.Timestamptz
	)
	err := row.Scan(&id, &a.Name, &a.Resource, &a.AllowCIMDClients, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return API{}, ErrNotFound
		}
		return API{}, err
	}

	a.ID = mustAPIID(id)
	a.CreatedAt = createdAt.Time
	a.UpdatedAt = datastore.TimePtr(updatedAt)
	return a, nil
}

// scanPermission scans one permission row.
func scanPermission(row scanner) (Permission, error) {
	var (
		id      string
		p       Permission
		desc    pgtype.Text
		created pgtype.Timestamptz
	)
	if err := row.Scan(&id, &p.Key, &p.Name, &desc, &p.AllowedForCIMDClients, &created); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Permission{}, ErrNotFound
		}
		return Permission{}, err
	}
	p.ID = id
	if desc.Valid {
		text := desc.String
		p.Description = &text
	}
	p.CreatedAt = created.Time
	return p, nil
}

// scanClient scans one apiClientDto projection row.
func scanClient(row scanner) (ClientRef, error) {
	var c ClientRef
	if err := row.Scan(&c.ID, &c.Name, &c.ClientType, &c.IsPublic, &c.HasLogo, &c.HasDarkLogo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ClientRef{}, ErrNotFound
		}
		return ClientRef{}, err
	}
	return c, nil
}

func mustAPIID(uuidText string) APIID {
	id, err := typeid.FromUUID[APIID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("apiaccess: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

func mapErr(err error) error {
	return datastore.MapErr(err, "apiaccess store", ErrNotFound, ErrDuplicate)
}
