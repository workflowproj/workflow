// Package scim is a SCIM 2.0 service provider (RFC 7643 / 7644) for identity providers that push
// users and groups: Okta, Microsoft Entra ID, OneLogin, JumpCloud, Keycloak. Each IdP connection
// has its own bearer token. Users are matched across connections; groups belong to a connection.
package scim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/identity"
	"github.com/geoffo-dev/workflow-management/internal/secure"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

const (
	SchemaUser       = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup      = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaEnterprise = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	SchemaList       = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaError      = "urn:ietf:params:scim:api:messages:2.0:Error"
	SchemaPatch      = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	contentType      = "application/scim+json; charset=utf-8"
	maxCount         = 1000
)

type Handler struct {
	Pool    *pgxpool.Pool
	Q       *dbq.Queries
	Prov    *identity.Provisioner
	BaseURL string // e.g. https://hooks.example.com/scim/v2
	Log     *slog.Logger
}

// Error is a SCIM protocol error.
type Error struct {
	Status   int
	ScimType string
	Detail   string
}

func (e *Error) Error() string { return e.Detail }

func errf(status int, scimType, format string, args ...any) *Error {
	return &Error{Status: status, ScimType: scimType, Detail: fmt.Sprintf(format, args...)}
}

type connKey struct{}

func connectionFrom(ctx context.Context) dbq.ScimConnection {
	c, _ := ctx.Value(connKey{}).(dbq.ScimConnection)
	return c
}

// Routes registers the SCIM endpoints on mux under prefix (e.g. "/scim/v2").
func (h *Handler) Routes(mux *http.ServeMux, prefix string) {
	handle := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) (any, int, error)) {
		method, path, _ := strings.Cut(pattern, " ")
		mux.Handle(method+" "+prefix+path, h.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, status, err := fn(w, r)
			if err != nil {
				h.writeError(w, r, err)
				return
			}
			if status == http.StatusNoContent {
				w.WriteHeader(status)
				return
			}
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		})))
	}
	handle("GET /ServiceProviderConfig", h.serviceProviderConfig)
	handle("GET /ResourceTypes", h.resourceTypes)
	handle("GET /ResourceTypes/{id}", h.resourceType)
	handle("GET /Schemas", h.schemas)
	handle("GET /Schemas/{id}", h.schema)
	handle("GET /Users", h.listUsers)
	handle("POST /Users", h.createUser)
	handle("GET /Users/{id}", h.getUser)
	handle("PUT /Users/{id}", h.replaceUser)
	handle("PATCH /Users/{id}", h.patchUser)
	handle("DELETE /Users/{id}", h.deleteUser)
	handle("GET /Groups", h.listGroups)
	handle("POST /Groups", h.createGroup)
	handle("GET /Groups/{id}", h.getGroup)
	handle("PUT /Groups/{id}", h.replaceGroup)
	handle("PATCH /Groups/{id}", h.patchGroup)
	handle("DELETE /Groups/{id}", h.deleteGroup)
}

func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok {
			h.writeError(w, r, errf(http.StatusUnauthorized, "", "bearer token required"))
			return
		}
		conn, err := h.Q.GetScimConnectionByToken(r.Context(), secure.Hash(strings.TrimSpace(token)))
		if err != nil {
			h.writeError(w, r, errf(http.StatusUnauthorized, "", "invalid token"))
			return
		}
		if conn.LastUsedAt == nil || time.Since(*conn.LastUsedAt) > time.Minute {
			_ = h.Q.TouchScimConnection(r.Context(), conn.ID)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), connKey{}, conn)))
	})
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var se *Error
	if !errors.As(err, &se) {
		h.Log.Error("scim request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		se = errf(http.StatusInternalServerError, "", "internal error")
	}
	body := map[string]any{"schemas": []string{SchemaError}, "status": strconv.Itoa(se.Status), "detail": se.Detail}
	if se.ScimType != "" {
		body["scimType"] = se.ScimType
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(se.Status)
	_ = json.NewEncoder(w).Encode(body)
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return errf(http.StatusBadRequest, "invalidSyntax", "request body is not valid JSON: %v", err)
	}
	return nil
}

func (h *Handler) audit(ctx context.Context, action, targetType, targetID string, data map[string]any) {
	conn := connectionFrom(ctx)
	if data == nil {
		data = map[string]any{}
	}
	data["connection"] = conn.Name
	b, _ := json.Marshal(data)
	_ = h.Q.InsertAudit(ctx, dbq.InsertAuditParams{ActorType: "scim", Action: action, TargetType: &targetType, TargetID: &targetID, Data: b})
}

// ---------------------------------------------------------------------------
// Listing helpers

type listParams struct {
	filter     string
	startIndex int
	count      int
	attributes []string
	excluded   []string
}

func parseList(r *http.Request) listParams {
	q := r.URL.Query()
	lp := listParams{filter: q.Get("filter"), startIndex: 1, count: 100}
	if n, err := strconv.Atoi(q.Get("startIndex")); err == nil && n > 0 {
		lp.startIndex = n
	}
	if n, err := strconv.Atoi(q.Get("count")); err == nil && n >= 0 {
		lp.count = min(n, maxCount)
	}
	lp.attributes = splitAttrs(q.Get("attributes"))
	lp.excluded = splitAttrs(q.Get("excludedAttributes"))
	return lp
}

func splitAttrs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}

// project applies `attributes` / `excludedAttributes` to top-level attributes.
func project(res map[string]any, attributes, excluded []string) map[string]any {
	always := map[string]bool{"id": true, "schemas": true, "meta": true}
	if len(attributes) > 0 {
		keep := map[string]bool{}
		for _, a := range attributes {
			keep[strings.SplitN(a, ".", 2)[0]] = true
		}
		out := map[string]any{}
		for k, v := range res {
			if always[k] || keep[strings.ToLower(k)] {
				out[k] = v
			}
		}
		return out
	}
	for _, e := range excluded {
		for k := range res {
			if strings.EqualFold(k, e) && !always[k] {
				delete(res, k)
			}
		}
	}
	return res
}

func listResponse(resources []map[string]any, lp listParams) map[string]any {
	total := len(resources)
	start := min(lp.startIndex-1, total)
	end := min(start+lp.count, total)
	page := resources[start:end]
	if page == nil {
		page = []map[string]any{}
	}
	for i := range page {
		page[i] = project(page[i], lp.attributes, lp.excluded)
	}
	return map[string]any{
		"schemas": []string{SchemaList}, "totalResults": total, "startIndex": lp.startIndex,
		"itemsPerPage": len(page), "Resources": page,
	}
}

func meta(resourceType, location string, created, modified time.Time) map[string]any {
	return map[string]any{
		"resourceType": resourceType, "created": created.UTC().Format(time.RFC3339), "lastModified": modified.UTC().Format(time.RFC3339),
		"location": location, "version": fmt.Sprintf("W/\"%d\"", modified.UnixNano()),
	}
}

func parseID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return id, errf(http.StatusNotFound, "", "resource not found")
	}
	return id, nil
}

func (h *Handler) tx(ctx context.Context, fn func(tx pgx.Tx, q *dbq.Queries) error) error {
	return db.Tx(ctx, h.Pool, fn)
}

var systemActor = authz.SystemActor()

func audit(ctx context.Context, q *dbq.Queries, action, id string, data map[string]any) {
	tracker.Audit(ctx, q, systemActor, action, "user", id, nil, data)
}
