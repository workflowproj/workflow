package scim

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	filter "github.com/scim2/filter-parser/v2"

	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

func (h *Handler) groupResource(g dbq.Group, members []dbq.User) map[string]any {
	ms := []any{}
	for _, m := range members {
		ms = append(ms, map[string]any{"value": m.ID.String(), "display": m.DisplayName, "$ref": h.BaseURL + "/Users/" + m.ID.String()})
	}
	res := map[string]any{
		"schemas":     []any{SchemaGroup},
		"id":          g.ID.String(),
		"displayName": g.DisplayName,
		"members":     ms,
		"meta":        meta("Group", h.BaseURL+"/Groups/"+g.ID.String(), g.CreatedAt, g.UpdatedAt),
	}
	if g.ExternalID != nil {
		res["externalId"] = *g.ExternalID
	}
	return res
}

func (h *Handler) loadGroup(ctx context.Context, q *dbq.Queries, r *http.Request) (dbq.Group, error) {
	id, err := parseID(r)
	if err != nil {
		return dbq.Group{}, err
	}
	g, err := q.GetGroup(ctx, id)
	conn := connectionFrom(ctx)
	if err != nil || g.Source != "scim" || g.ScimConnectionID == nil || *g.ScimConnectionID != conn.ID {
		return g, errf(http.StatusNotFound, "", "group %s not found", r.PathValue("id"))
	}
	return g, nil
}

func wantsMembers(r *http.Request) bool {
	lp := parseList(r)
	for _, e := range lp.excluded {
		if e == "members" {
			return false
		}
	}
	if len(lp.attributes) > 0 {
		for _, a := range lp.attributes {
			if strings.HasPrefix(a, "members") {
				return true
			}
		}
		return false
	}
	return true
}

func (h *Handler) renderGroup(ctx context.Context, q *dbq.Queries, g dbq.Group, r *http.Request) (map[string]any, error) {
	var members []dbq.User
	if wantsMembers(r) {
		var err error
		if members, err = q.GroupMembers(ctx, g.ID); err != nil {
			return nil, err
		}
	}
	lp := parseList(r)
	return project(h.groupResource(g, members), lp.attributes, lp.excluded), nil
}

func (h *Handler) listGroups(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	conn := connectionFrom(ctx)
	lp := parseList(r)
	expr, err := parseFilter(lp.filter)
	if err != nil {
		return nil, 0, err
	}
	all, err := h.Q.ListGroupsBySource(ctx, "scim")
	if err != nil {
		return nil, 0, err
	}
	withMembers := wantsMembers(r)
	var out []map[string]any
	for _, g := range all {
		if g.ScimConnectionID == nil || *g.ScimConnectionID != conn.ID {
			continue
		}
		var members []dbq.User
		if withMembers {
			if members, err = h.Q.GroupMembers(ctx, g.ID); err != nil {
				return nil, 0, err
			}
		}
		res := h.groupResource(g, members)
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

func (h *Handler) getGroup(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	g, err := h.loadGroup(r.Context(), h.Q, r)
	if err != nil {
		return nil, 0, err
	}
	res, err := h.renderGroup(r.Context(), h.Q, g, r)
	return res, http.StatusOK, err
}

// memberIDs extracts user ids from [{value: id}, ...]; unknown or deleted users are skipped.
func (h *Handler) memberIDs(ctx context.Context, q *dbq.Queries, v any) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	for _, e := range asList(v) {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, errf(http.StatusBadRequest, "invalidValue", "members must be objects with a value")
		}
		s, _ := m["value"].(string)
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, errf(http.StatusBadRequest, "invalidValue", "member value %q is not a user id", s)
		}
		u, err := q.GetUser(ctx, id)
		if err != nil || u.DeletedAt != nil {
			h.Log.Warn("scim group member not found; skipping", "user", id)
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (h *Handler) setMembers(ctx context.Context, q *dbq.Queries, groupID uuid.UUID, ids []uuid.UUID) error {
	if err := q.ClearGroupMembers(ctx, groupID); err != nil {
		return err
	}
	for _, id := range ids {
		if err := q.AddGroupMember(ctx, dbq.AddGroupMemberParams{GroupID: groupID, UserID: id}); err != nil {
			return err
		}
	}
	return nil
}

type groupBody struct {
	DisplayName string  `json:"displayName"`
	ExternalID  *string `json:"externalId"`
	Members     any     `json:"members"`
}

func (h *Handler) createGroup(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	conn := connectionFrom(ctx)
	var body groupBody
	if err := decode(r, &body); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(body.DisplayName) == "" {
		return nil, 0, errf(http.StatusBadRequest, "invalidValue", "displayName is required")
	}
	var res map[string]any
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		g, err := q.CreateGroup(ctx, dbq.CreateGroupParams{DisplayName: strings.TrimSpace(body.DisplayName), Source: "scim", ScimConnectionID: &conn.ID, ExternalID: body.ExternalID})
		if err != nil {
			if db.IsUniqueViolation(err) {
				return errf(http.StatusConflict, "uniqueness", "a group named %q already exists", body.DisplayName)
			}
			return err
		}
		ids, err := h.memberIDs(ctx, q, body.Members)
		if err != nil {
			return err
		}
		if err := h.setMembers(ctx, q, g.ID, ids); err != nil {
			return err
		}
		audit(ctx, q, "scim.group_create", g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(ids), "connection": conn.Name})
		res, err = h.renderGroup(ctx, q, g, r)
		return err
	})
	return res, http.StatusCreated, err
}

func (h *Handler) replaceGroup(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	var body groupBody
	if err := decode(r, &body); err != nil {
		return nil, 0, err
	}
	var res map[string]any
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		g, err := h.loadGroup(ctx, q, r)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(body.DisplayName)
		if name == "" {
			name = g.DisplayName
		}
		if g, err = q.UpdateGroup(ctx, dbq.UpdateGroupParams{ID: g.ID, DisplayName: name, Description: g.Description, ExternalID: body.ExternalID, Email: g.Email}); err != nil {
			if db.IsUniqueViolation(err) {
				return errf(http.StatusConflict, "uniqueness", "a group named %q already exists", name)
			}
			return err
		}
		ids, err := h.memberIDs(ctx, q, body.Members)
		if err != nil {
			return err
		}
		if err := h.setMembers(ctx, q, g.ID, ids); err != nil {
			return err
		}
		audit(ctx, q, "scim.group_replace", g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(ids), "connection": connectionFrom(ctx).Name})
		res, err = h.renderGroup(ctx, q, g, r)
		return err
	})
	return res, http.StatusOK, err
}

func (h *Handler) patchGroup(w http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	var body patchRequest
	if err := decode(r, &body); err != nil {
		return nil, 0, err
	}
	var res map[string]any
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		g, err := h.loadGroup(ctx, q, r)
		if err != nil {
			return err
		}
		name, ext := g.DisplayName, g.ExternalID
		current, err := q.GroupMemberIDs(ctx, g.ID)
		if err != nil {
			return err
		}
		members := map[uuid.UUID]bool{}
		for _, id := range current {
			members[id] = true
		}
		for _, op := range body.Operations {
			opName := strings.ToLower(op.Op)
			path := strings.TrimSpace(op.Path)
			switch {
			case path == "" && (opName == "replace" || opName == "add"):
				m, ok := op.Value.(map[string]any)
				if !ok {
					return errf(http.StatusBadRequest, "invalidValue", "a %s without a path needs an object value", op.Op)
				}
				for k, v := range m {
					switch strings.ToLower(k) {
					case "displayname":
						if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
							name = strings.TrimSpace(s)
						}
					case "externalid":
						if s, ok := v.(string); ok {
							ext = &s
						}
					case "members":
						ids, err := h.memberIDs(ctx, q, v)
						if err != nil {
							return err
						}
						if opName == "replace" {
							members = map[uuid.UUID]bool{}
						}
						for _, id := range ids {
							members[id] = true
						}
					}
				}
			case strings.EqualFold(path, "displayName"):
				if s, ok := op.Value.(string); ok && strings.TrimSpace(s) != "" {
					name = strings.TrimSpace(s)
				}
			case strings.EqualFold(path, "externalId"):
				if opName == "remove" {
					ext = nil
				} else if s, ok := op.Value.(string); ok {
					ext = &s
				}
			case strings.EqualFold(path, "members"):
				ids, err := h.memberIDs(ctx, q, op.Value)
				if err != nil {
					return err
				}
				switch opName {
				case "add":
					for _, id := range ids {
						members[id] = true
					}
				case "replace":
					members = map[uuid.UUID]bool{}
					for _, id := range ids {
						members[id] = true
					}
				case "remove":
					if op.Value == nil {
						members = map[uuid.UUID]bool{} // remove all members
					}
					for _, id := range ids {
						delete(members, id)
					}
				}
			case strings.HasPrefix(strings.ToLower(path), "members["):
				// members[value eq "id"]
				p, err := filter.ParsePath([]byte(path))
				if err != nil || opName != "remove" {
					return errf(http.StatusBadRequest, "invalidPath", "unsupported path %q", path)
				}
				for id := range members {
					ok, err := matches(p.ValueExpression, map[string]any{"value": id.String()})
					if err != nil {
						return err
					}
					if ok {
						delete(members, id)
					}
				}
			default:
				return errf(http.StatusBadRequest, "invalidPath", "unsupported path %q", path)
			}
		}
		if g, err = q.UpdateGroup(ctx, dbq.UpdateGroupParams{ID: g.ID, DisplayName: name, Description: g.Description, ExternalID: ext, Email: g.Email}); err != nil {
			if db.IsUniqueViolation(err) {
				return errf(http.StatusConflict, "uniqueness", "a group named %q already exists", name)
			}
			return err
		}
		ids := make([]uuid.UUID, 0, len(members))
		for id := range members {
			ids = append(ids, id)
		}
		if err := h.setMembers(ctx, q, g.ID, ids); err != nil {
			return err
		}
		audit(ctx, q, "scim.group_patch", g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(ids), "connection": connectionFrom(ctx).Name})
		res, err = h.renderGroup(ctx, q, g, r)
		return err
	})
	return res, http.StatusOK, err
}

func (h *Handler) deleteGroup(_ http.ResponseWriter, r *http.Request) (any, int, error) {
	ctx := r.Context()
	err := h.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		g, err := h.loadGroup(ctx, q, r)
		if err != nil {
			return err
		}
		if err := q.DeleteGroup(ctx, g.ID); err != nil {
			return err
		}
		audit(ctx, q, "scim.group_delete", g.ID.String(), map[string]any{"displayName": g.DisplayName, "connection": connectionFrom(ctx).Name})
		return nil
	})
	return nil, http.StatusNoContent, err
}
