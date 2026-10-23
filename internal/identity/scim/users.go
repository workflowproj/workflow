package scim

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	filter "github.com/scim2/filter-parser/v2"

	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

type userAttrs struct {
	UserName    string
	Email       string
	DisplayName string
	GivenName   string
	FamilyName  string
	ExternalID  *string
	Active      bool
}

func (h *Handler) userResource(u dbq.User, groups []dbq.Group) map[string]any {
	loc := h.BaseURL + "/Users/" + u.ID.String()
	name := map[string]any{"formatted": u.DisplayName}
	if u.GivenName != nil {
		name["givenName"] = *u.GivenName
	}
	if u.FamilyName != nil {
		name["familyName"] = *u.FamilyName
	}
	gs := []any{}
	for _, g := range groups {
		gs = append(gs, map[string]any{"value": g.ID.String(), "display": g.DisplayName, "$ref": h.BaseURL + "/Groups/" + g.ID.String()})
	}
	res := map[string]any{
		"schemas":     []any{SchemaUser},
		"id":          u.ID.String(),
		"userName":    u.Username,
		"name":        name,
		"displayName": u.DisplayName,
		"emails":      []any{map[string]any{"value": u.Email, "type": "work", "primary": true}},
		"active":      u.Active,
		"groups":      gs,
		"meta":        meta("User", loc, u.CreatedAt, u.UpdatedAt),
	}
	if u.ScimExternalID != nil {
		res["externalId"] = *u.ScimExternalID
	}
	return res
}

// attrsFromUser is the starting point for PATCH.
func attrsFromUser(u dbq.User) userAttrs {
	a := userAttrs{UserName: u.Username, Email: u.Email, DisplayName: u.DisplayName, ExternalID: u.ScimExternalID, Active: u.Active}
	if u.GivenName != nil {
		a.GivenName = *u.GivenName
	}
	if u.FamilyName != nil {
		a.FamilyName = *u.FamilyName
	}
	return a
}

// applyUserObject sets attributes from a (partial) User representation.
func applyUserObject(a *userAttrs, m map[string]any) error {
	for k, v := range m {
		if err := applyUserAttr(a, k, v); err != nil {
			return err
		}
	}
	return nil
}

// applyUserAttr handles one attribute path such as "userName", "name.givenName",
// "emails[type eq \"work\"].value" or "active". Unknown attributes are ignored, because IdPs
// routinely send attributes (title, department, ...) a service provider does not store.
func applyUserAttr(a *userAttrs, path string, v any) error {
	p := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(path, SchemaUser+":"), SchemaUser+"."))
	str := func() (string, error) {
		switch x := v.(type) {
		case string:
			return x, nil
		case nil:
			return "", nil
		}
		return "", errf(http.StatusBadRequest, "invalidValue", "%s must be a string", path)
	}
	switch {
	case p == "username":
		s, err := str()
		if err != nil {
			return err
		}
		a.UserName = strings.TrimSpace(s)
	case p == "displayname":
		s, err := str()
		if err != nil {
			return err
		}
		a.DisplayName = strings.TrimSpace(s)
	case p == "externalid":
		s, err := str()
		if err != nil {
			return err
		}
		if s == "" {
			a.ExternalID = nil
		} else {
			a.ExternalID = &s
		}
	case p == "active":
		b, ok := toBool(v)
		if !ok {
			return errf(http.StatusBadRequest, "invalidValue", "active must be a boolean")
		}
		a.Active = b
	case p == "name":
		m, _ := v.(map[string]any)
		for k, sv := range m {
			if err := applyUserAttr(a, "name."+k, sv); err != nil {
				return err
			}
		}
	case p == "name.givenname":
		s, err := str()
		if err != nil {
			return err
		}
		a.GivenName = s
	case p == "name.familyname":
		s, err := str()
		if err != nil {
			return err
		}
		a.FamilyName = s
	case p == "name.formatted":
		s, err := str()
		if err != nil {
			return err
		}
		if a.DisplayName == "" {
			a.DisplayName = s
		}
	case p == "emails" || strings.HasPrefix(p, "emails[") || p == "emails.value":
		if email := pickEmail(v); email != "" {
			a.Email = strings.ToLower(email)
		}
	}
	return nil
}

// pickEmail chooses the primary, then work, then first email from a value or list of values.
func pickEmail(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		s, _ := x["value"].(string)
		return s
	case []any:
		best, rank := "", 9
		for _, e := range x {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			val, _ := m["value"].(string)
			if val == "" {
				continue
			}
			r := 3
			if p, _ := toBool(m["primary"]); p {
				r = 1
			} else if t, _ := m["type"].(string); strings.EqualFold(t, "work") {
				r = 2
			}
			if r < rank {
				best, rank = val, r
			}
		}
		return best
	}
	return ""
}

func (a *userAttrs) finalize() error {
	if a.UserName == "" {
		return errf(http.StatusBadRequest, "invalidValue", "userName is required")
	}
	if a.Email == "" && strings.Contains(a.UserName, "@") {
		a.Email = strings.ToLower(a.UserName)
	}
	if a.Email == "" || !strings.Contains(a.Email, "@") {
		return errf(http.StatusBadRequest, "invalidValue", "an email address is required (emails or an email-shaped userName)")
	}
	if a.DisplayName == "" {
		a.DisplayName = strings.TrimSpace(a.GivenName + " " + a.FamilyName)
	}
	if a.DisplayName == "" {
		a.DisplayName = a.UserName
	}
	return nil
}

// groupsForUsers returns this connection's groups each user belongs to.
func (h *Handler) groupsForUsers(ctx context.Context, connID uuid.UUID, userIDs []uuid.UUID) (map[uuid.UUID][]dbq.Group, error) {
	rows, err := h.Pool.Query(ctx, `
		SELECT gm.user_id, g.id, g.display_name FROM group_members gm JOIN groups g ON g.id = gm.group_id
		WHERE g.scim_connection_id = $1 AND gm.user_id = ANY($2) ORDER BY g.display_name`, connID, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]dbq.Group{}
	for rows.Next() {
		var uid uuid.UUID
		var g dbq.Group
		if err := rows.Scan(&uid, &g.ID, &g.DisplayName); err != nil {
			return nil, err
		}
		out[uid] = append(out[uid], g)
	}
	return out, rows.Err()
}

const userColumns = `id, kind, email, username, display_name, given_name, family_name, avatar_url, org_role, active, source,
	google_id, scim_connection_id, scim_external_id, deactivated_at, deleted_at, anonymized_at, last_login_at, created_at, updated_at`

func scanUsers(rows pgx.Rows) ([]dbq.User, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (dbq.User, error) {
		var u dbq.User
		err := r.Scan(&u.ID, &u.Kind, &u.Email, &u.Username, &u.DisplayName, &u.GivenName, &u.FamilyName, &u.AvatarUrl, &u.OrgRole,
			&u.Active, &u.Source, &u.GoogleID, &u.ScimConnectionID, &u.ScimExternalID, &u.DeactivatedAt, &u.DeletedAt, &u.AnonymizedAt,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
		return u, err
	})
}

func (h *Handler) listUsers(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	conn := connectionFrom(ctx)
	lp := parseList(r)
	expr, err := parseFilter(lp.filter)
	if err != nil {
		return nil, 0, err
	}
	query := `SELECT ` + userColumns + ` FROM users WHERE kind = 'human' AND deleted_at IS NULL`
	var args []any
	if attr, val, ok := simpleEq(expr); ok {
		switch attr {
		case "username":
			query += ` AND lower(username) = lower($1)`
			args = append(args, val)
		case "externalid":
			query += ` AND scim_external_id = $1`
			args = append(args, val)
		case "emails", "emails.value":
			query += ` AND email = lower($1)`
			args = append(args, val)
		case "id":
			id, err := uuid.Parse(val)
			if err != nil {
				return listResponse(nil, lp), http.StatusOK, nil
			}
			query += ` AND id = $1`
			args = append(args, id)
		}
	}
	rows, err := h.Pool.Query(ctx, query+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, 0, err
	}
	users, err := scanUsers(rows)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]uuid.UUID, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	groups, err := h.groupsForUsers(ctx, conn.ID, ids)
	if err != nil {
		return nil, 0, err
	}
	var out []map[string]any
	for _, u := range users {
		res := h.userResource(u, groups[u.ID])
		ok, err := matches(expr, res)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			out = append(out, res)
		}
	}
	return listResponse(out, lp), http.StatusOK, nil
}

func (h *Handler) loadUser(ctx context.Context, q *dbq.Queries, r *http.Request) (dbq.User, error) {
	id, err := parseID(r)
	if err != nil {
		return dbq.User{}, err
	}
	u, err := q.GetUser(ctx, id)
	if err != nil || u.DeletedAt != nil || u.Kind != "human" {
		return u, errf(http.StatusNotFound, "", "user %s not found", r.PathValue("id"))
	}
	return u, nil
}

func (h *Handler) renderUser(ctx context.Context, u dbq.User, r *http.Request) (map[string]any, error) {
	groups, err := h.groupsForUsers(ctx, connectionFrom(ctx).ID, []uuid.UUID{u.ID})
	if err != nil {
		return nil, err
	}
	lp := parseList(r)
	return project(h.userResource(u, groups[u.ID]), lp.attributes, lp.excluded), nil
}

func (h *Handler) getUser(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	u, err := h.loadUser(r.Context(), h.Q, r)
	if err != nil {
		return nil, 0, err
	}
	res, err := h.renderUser(r.Context(), u, r)
	return res, http.StatusOK, err
}

// writeUser inserts or updates a user from SCIM attributes, adopting it for this connection.
// A soft-deleted user with the same email or userName is restored (e.g. a re-hire).
func (h *Handler) writeUser(ctx context.Context, q *dbq.Queries, tx pgx.Tx, existing *dbq.User, a userAttrs) (dbq.User, error) {
	conn := connectionFrom(ctx)
	if err := a.finalize(); err != nil {
		return dbq.User{}, err
	}
	// Uniqueness against other live users.
	var clash uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE (email = $1 OR lower(username) = lower($2)) AND ($3::uuid IS NULL OR id <> $3) LIMIT 1`,
		a.Email, a.UserName, idOrNil(existing)).Scan(&clash)
	if err == nil {
		other, _ := q.GetUser(ctx, clash)
		if other.DeletedAt == nil || existing != nil {
			return dbq.User{}, errf(http.StatusConflict, "uniqueness", "a user with userName %q or email %q already exists", a.UserName, a.Email)
		}
		existing = &other // restore the soft-deleted account
	} else if !db.IsNoRows(err) {
		return dbq.User{}, err
	}
	err = nil
	role := "member"
	if h.Prov.IsBootstrapAdmin(a.Email) {
		role = "admin"
	}
	var u dbq.User
	if existing == nil {
		u, err = q.CreateUser(ctx, dbq.CreateUserParams{
			Kind: "human", Email: a.Email, Username: a.UserName, DisplayName: a.DisplayName, GivenName: nilIfEmpty(a.GivenName),
			FamilyName: nilIfEmpty(a.FamilyName), OrgRole: role, Source: "scim", ScimConnectionID: &conn.ID, ScimExternalID: a.ExternalID,
			Active: a.Active,
		})
	} else {
		rows, qerr := tx.Query(ctx, `UPDATE users SET email = $2, username = $3, display_name = $4, given_name = $5, family_name = $6,
				scim_external_id = $7, scim_connection_id = $8, source = 'scim', active = $9,
				deactivated_at = CASE WHEN $9 THEN NULL ELSE coalesce(deactivated_at, now()) END,
				deleted_at = NULL, org_role = CASE WHEN $10 THEN 'admin' ELSE org_role END, updated_at = now()
			WHERE id = $1 RETURNING `+userColumns,
			existing.ID, a.Email, a.UserName, a.DisplayName, nilIfEmpty(a.GivenName), nilIfEmpty(a.FamilyName), a.ExternalID, conn.ID, a.Active, role == "admin")
		if qerr != nil {
			return u, qerr
		}
		users, cerr := scanUsers(rows)
		if cerr != nil {
			err = cerr
		} else if len(users) == 1 {
			u = users[0]
		}
	}
	if err != nil {
		if db.IsUniqueViolation(err) {
			return u, errf(http.StatusConflict, "uniqueness", "a user with userName %q or externalId already exists", a.UserName)
		}
		return u, err
	}
	if !u.Active {
		if err := q.DeleteUserSessions(ctx, u.ID); err != nil {
			return u, err
		}
		if err := q.RevokeUserTokens(ctx, u.ID); err != nil {
			return u, err
		}
	}
	return u, nil
}

func idOrNil(u *dbq.User) *uuid.UUID {
	if u == nil {
		return nil
	}
	return &u.ID
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h *Handler) createUser(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	var body map[string]any
	if err := decode(r, &body); err != nil {
		return nil, 0, err
	}
	a := userAttrs{Active: true}
	if err := applyUserObject(&a, body); err != nil {
		return nil, 0, err
	}
	var u dbq.User
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		u, err = h.writeUser(ctx, q, tx, nil, a)
		if err == nil {
			audit(ctx, q, "scim.user_create", u.ID.String(), map[string]any{"userName": u.Username, "connection": connectionFrom(ctx).Name})
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	res, err := h.renderUser(ctx, u, r)
	return res, http.StatusCreated, err
}

func (h *Handler) replaceUser(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	var body map[string]any
	if err := decode(r, &body); err != nil {
		return nil, 0, err
	}
	var u dbq.User
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		cur, err := h.loadUser(ctx, q, r)
		if err != nil {
			return err
		}
		a := userAttrs{Active: true, Email: cur.Email}
		if err := applyUserObject(&a, body); err != nil {
			return err
		}
		if u, err = h.writeUser(ctx, q, tx, &cur, a); err == nil {
			audit(ctx, q, "scim.user_replace", u.ID.String(), map[string]any{"active": u.Active, "connection": connectionFrom(ctx).Name})
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	res, err := h.renderUser(ctx, u, r)
	return res, http.StatusOK, err
}

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type patchRequest struct {
	Schemas    []string  `json:"schemas"`
	Operations []patchOp `json:"Operations"`
}

func (h *Handler) patchUser(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	var body patchRequest
	if err := decode(r, &body); err != nil {
		return nil, 0, err
	}
	if len(body.Operations) == 0 {
		return nil, 0, errf(http.StatusBadRequest, "invalidValue", "no Operations")
	}
	var u dbq.User
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		cur, err := h.loadUser(ctx, q, r)
		if err != nil {
			return err
		}
		a := attrsFromUser(cur)
		for _, op := range body.Operations {
			switch strings.ToLower(op.Op) {
			case "add", "replace":
				if op.Path == "" {
					m, ok := op.Value.(map[string]any)
					if !ok {
						return errf(http.StatusBadRequest, "invalidValue", "a %s without a path needs an object value", op.Op)
					}
					if err := applyUserObject(&a, m); err != nil {
						return err
					}
					continue
				}
				if _, err := filter.ParsePath([]byte(op.Path)); err != nil {
					return errf(http.StatusBadRequest, "invalidPath", "invalid path %q", op.Path)
				}
				if err := applyUserAttr(&a, op.Path, op.Value); err != nil {
					return err
				}
			case "remove":
				switch strings.ToLower(op.Path) {
				case "externalid":
					a.ExternalID = nil
				case "name.givenname":
					a.GivenName = ""
				case "name.familyname":
					a.FamilyName = ""
				case "":
					return errf(http.StatusBadRequest, "noTarget", "remove requires a path")
				}
			default:
				return errf(http.StatusBadRequest, "invalidSyntax", "unknown op %q", op.Op)
			}
		}
		if u, err = h.writeUser(ctx, q, tx, &cur, a); err == nil {
			audit(ctx, q, "scim.user_patch", u.ID.String(), map[string]any{"active": u.Active, "connection": connectionFrom(ctx).Name})
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	res, err := h.renderUser(ctx, u, r)
	return res, http.StatusOK, err
}

func (h *Handler) deleteUser(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := h.loadUser(ctx, q, r)
		if err != nil {
			return err
		}
		if err := h.Prov.MarkDeleted(ctx, q, u.ID); err != nil {
			return err
		}
		audit(ctx, q, "scim.user_delete", u.ID.String(), map[string]any{"userName": u.Username, "connection": connectionFrom(ctx).Name})
		return nil
	})
	return nil, http.StatusNoContent, err
}
