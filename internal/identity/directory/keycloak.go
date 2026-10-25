package directory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/identity"
)

// KeycloakSyncer provisions people and groups from a Keycloak realm (Keycloak's own SCIM support
// only receives users, so Workflow pulls them through the Admin REST API).
//
// People get source "keycloak" and a user_identities row (provider Cfg.Provider, subject = their
// Keycloak user id, which is the sub claim when they sign in). A Keycloak user is matched to an
// account by that identity, else by verified email; SCIM-managed accounts are never touched.
// People disabled in Keycloak or outside the access group are deactivated, people deleted in
// Keycloak are marked deleted. Groups are named by their path without the leading "/"
// ("engineering/platform"), keep their Keycloak id as external_id and mirror direct members.
type KeycloakSyncer struct {
	Pool   *pgxpool.Pool
	Q      *dbq.Queries
	Cfg    config.Keycloak
	Prov   *identity.Provisioner
	Log    *slog.Logger
	Client *KeycloakClient
}

func NewKeycloakSyncer(pool *pgxpool.Pool, cfg config.Keycloak, prov *identity.Provisioner, log *slog.Logger) *KeycloakSyncer {
	return &KeycloakSyncer{Pool: pool, Q: dbq.New(pool), Cfg: cfg, Prov: prov, Log: log, Client: NewKeycloakClient(cfg)}
}

// Source names this directory in directory_sync_runs.
func (s *KeycloakSyncer) Source() string { return "keycloak" }

// Run performs one sync and records it in directory_sync_runs.
func (s *KeycloakSyncer) Run(ctx context.Context, trigger string) (Stats, error) {
	return recordRun(ctx, s.Q, s.Source(), trigger, s.sync)
}

func (s *KeycloakSyncer) sync(ctx context.Context) (Stats, error) {
	stats := Stats{}
	all, err := s.Client.ListUsers(ctx)
	if err != nil {
		return stats, err
	}
	upstream := map[string]bool{}
	for _, ku := range all {
		upstream[ku.ID] = true
	}
	inScope, err := s.scope(ctx, all)
	if err != nil {
		return stats, err
	}

	seen := map[uuid.UUID]bool{}
	local := map[string]uuid.UUID{} // Keycloak user id -> account, for group memberships
	for _, ku := range all {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if !inScope[ku.ID] || ku.ServiceAccountClientID != "" || strings.HasPrefix(ku.Username, "service-account-") {
			continue
		}
		id, err := s.upsertUser(ctx, ku, upstream, stats)
		if err != nil {
			return stats, fmt.Errorf("user %s: %w", ku.Username, err)
		}
		if id != uuid.Nil {
			seen[id] = true
			local[ku.ID] = id
		}
	}
	stats["users"] = len(seen)

	// Accounts this directory provisioned that it no longer offers: deactivated while the person
	// still exists in Keycloak (disabled is handled above; here: out of scope), else deleted.
	rows, err := s.Q.ListUsersBySourceWithIdentity(ctx, dbq.ListUsersBySourceWithIdentityParams{Provider: s.Cfg.Provider, Source: "keycloak"})
	if err != nil {
		return stats, err
	}
	type leftover struct {
		active   bool
		subjects []string
	}
	gone := map[uuid.UUID]*leftover{}
	var order []uuid.UUID
	for _, r := range rows {
		if seen[r.ID] {
			continue
		}
		l := gone[r.ID]
		if l == nil {
			l = &leftover{active: r.Active}
			gone[r.ID] = l
			order = append(order, r.ID)
		}
		if r.Subject != nil {
			l.subjects = append(l.subjects, *r.Subject)
		}
	}
	for _, id := range order {
		l := gone[id]
		if !l.active && slices.ContainsFunc(l.subjects, func(sub string) bool { return upstream[sub] }) {
			continue // already deactivated, and still in Keycloak: nothing to do
		}
		subject, exists, inScope, err := s.confirm(ctx, l.subjects, upstream)
		if err != nil {
			return stats, err
		}
		if exists && inScope {
			// Keycloak has them in scope after all: the listing shifted while people were added or
			// removed. The next run sees them again.
			s.Log.Warn("keycloak user missing from the listing but still in scope; left as is", "user_id", id)
			stats["users"]++
			local[subject] = id
			continue
		}
		err = db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
			if exists {
				if !l.active {
					return nil
				}
				_, err := s.Prov.Deactivate(ctx, q, id)
				return err
			}
			return s.Prov.MarkDeleted(ctx, q, id)
		})
		if err != nil {
			return stats, err
		}
		switch {
		case !exists:
			stats["usersDeleted"]++
		case l.active:
			stats["usersDeactivated"]++
		}
	}

	if s.Cfg.SyncGroups {
		if err := s.syncGroups(ctx, local, stats); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// scope returns the Keycloak user ids that should have access: members of the access group and
// of its subgroups, or everyone when no access group is set.
func (s *KeycloakSyncer) scope(ctx context.Context, all []KeycloakUser) (map[string]bool, error) {
	in := map[string]bool{}
	if s.Cfg.SyncGroup == "" {
		for _, u := range all {
			in[u.ID] = true
		}
		return in, nil
	}
	g, err := s.Client.GroupByPath(ctx, s.Cfg.SyncGroup)
	if errors.Is(err, errKeycloakNotFound) {
		return nil, fmt.Errorf("access group %s (WFM_KEYCLOAK_SYNC_GROUP) does not exist in realm %s", s.Cfg.SyncGroup, s.Cfg.Realm)
	}
	if err != nil {
		return nil, fmt.Errorf("access group %s: %w", s.Cfg.SyncGroup, err)
	}
	subs, err := s.Client.Subgroups(ctx, g)
	if err != nil {
		return nil, fmt.Errorf("access group %s: %w", s.Cfg.SyncGroup, err)
	}
	for _, grp := range append([]KeycloakGroup{g}, subs...) {
		ids, err := s.Client.ListMemberIDs(ctx, grp.ID)
		if err != nil {
			return nil, fmt.Errorf("access group %s: %w", grp.Path, err)
		}
		for _, id := range ids {
			in[id] = true
		}
	}
	return in, nil
}

// confirm asks Keycloak directly about an account the listings did not offer (by its Keycloak
// user ids): whether the person still exists, and whether they are in scope. Offset paging can
// skip someone while users or members are deleted mid-run, so a removal is never taken from a
// listing alone.
func (s *KeycloakSyncer) confirm(ctx context.Context, subjects []string, upstream map[string]bool) (subject string, exists, inScope bool, err error) {
	for _, sub := range subjects {
		if !upstream[sub] {
			u, err := s.Client.GetUser(ctx, sub)
			if errors.Is(err, errKeycloakNotFound) {
				continue
			}
			if err != nil {
				return "", false, false, err
			}
			if u.ServiceAccountClientID != "" {
				continue
			}
		}
		exists = true
		if s.Cfg.SyncGroup == "" {
			return sub, true, true, nil
		}
		paths, err := s.Client.UserGroupPaths(ctx, sub)
		if err != nil {
			return "", false, false, err
		}
		for _, p := range paths {
			if p == s.Cfg.SyncGroup || strings.HasPrefix(p, s.Cfg.SyncGroup+"/") {
				return sub, true, true, nil
			}
		}
	}
	return "", exists, false, nil
}

// errSkipUser rolls back one person's changes; they are counted in usersSkipped.
var errSkipUser = errors.New("skip user")

// upsertUser creates, links or updates the account of one in-scope Keycloak user and returns
// its id (uuid.Nil when the person has no account here).
func (s *KeycloakSyncer) upsertUser(ctx context.Context, ku KeycloakUser, upstream map[string]bool, stats Stats) (uuid.UUID, error) {
	email := strings.ToLower(strings.TrimSpace(ku.Email))
	// An unverified address is never used to find or name an account: anyone who can edit their
	// own Keycloak profile could otherwise take over the account with that email. Sign-in
	// requires a verified email for the same reason.
	verified := email != "" && ku.EmailVerified
	inactive := !ku.Enabled
	given, family := strings.TrimSpace(ku.FirstName), strings.TrimSpace(ku.LastName)
	name := strings.TrimSpace(given + " " + family)
	if name == "" {
		name = ku.Username
	}
	if name == "" {
		name = email
	}
	delta := Stats{}
	var id uuid.UUID
	err := db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		u, err := q.GetUserByIdentity(ctx, dbq.GetUserByIdentityParams{Provider: s.Cfg.Provider, Subject: ku.ID})
		if err == nil && u.AnonymizedAt != nil {
			err = pgx.ErrNoRows // an anonymised account is never revived; the person starts afresh
		}
		linked := err == nil
		if db.IsNoRows(err) {
			if !verified {
				if !inactive {
					s.Log.Warn("keycloak user has no verified email; not provisioned", "user", ku.Username, "id", ku.ID)
					return errSkipUser
				}
				return nil
			}
			u, err = q.GetUserByEmail(ctx, email)
		}
		switch {
		case db.IsNoRows(err):
			if inactive {
				return nil // never create accounts for disabled people
			}
			role := "member"
			if s.Prov.IsBootstrapAdmin(email) {
				role = "admin"
			}
			u, err = q.CreateUser(ctx, dbq.CreateUserParams{
				Kind: "human", Email: email, Username: email, DisplayName: name, GivenName: nilIfEmpty(given),
				FamilyName: nilIfEmpty(family), OrgRole: role, Source: "keycloak", Active: true,
			})
			if db.IsUniqueViolation(err) {
				s.Log.Warn("keycloak user clashes with an existing username; not provisioned", "user", ku.Username, "id", ku.ID)
				return errSkipUser
			}
			if err != nil {
				return err
			}
			delta["usersCreated"]++
			id = u.ID
			return linkIdentity(ctx, tx, u.ID, s.Cfg.Provider, ku.ID, email)
		case err != nil:
			return err
		}
		// SCIM-managed accounts belong to their identity provider, service accounts to their
		// owners; both are left alone.
		if u.Source == "scim" || u.Kind != "human" {
			s.Log.Info("keycloak user matches an account managed elsewhere; left alone", "user", ku.Username, "source", u.Source, "kind", u.Kind)
			return errSkipUser
		}
		if !linked {
			// Matched by email: refuse when the account already belongs to another live Keycloak
			// user (a realm that allows duplicate emails).
			var other string
			err := tx.QueryRow(ctx, `SELECT subject FROM user_identities WHERE user_id = $1 AND provider = $2 AND subject <> $3`, u.ID, s.Cfg.Provider, ku.ID).Scan(&other)
			if err == nil && upstream[other] {
				s.Log.Warn("keycloak user's email belongs to an account linked to another keycloak user; not linked", "user", ku.Username, "id", ku.ID)
				return errSkipUser
			}
			if err != nil && !db.IsNoRows(err) {
				return err
			}
		}
		id = u.ID
		if err := linkIdentity(ctx, tx, u.ID, s.Cfg.Provider, ku.ID, email); err != nil {
			return err
		}

		newEmail := u.Email
		if verified && email != u.Email {
			var clash bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id <> $1 AND (email = $2 OR lower(username) = $2))`, u.ID, email).Scan(&clash); err != nil {
				return err
			}
			if clash {
				s.Log.Warn("keycloak user's new email is used by another account; email not changed", "user", ku.Username, "id", ku.ID)
			} else {
				newEmail = email
			}
		}
		if u.Source != "keycloak" || newEmail != u.Email || u.DisplayName != name || deref(u.GivenName) != given ||
			deref(u.FamilyName) != family || u.DeletedAt != nil {
			if _, err := tx.Exec(ctx, `UPDATE users SET source = 'keycloak', email = $2,
					username = CASE WHEN username = email THEN $2 ELSE username END,
					display_name = $3, given_name = $4, family_name = $5, deleted_at = NULL, updated_at = now()
				WHERE id = $1`, u.ID, newEmail, name, nilIfEmpty(given), nilIfEmpty(family)); err != nil {
				return err
			}
			delta["usersUpdated"]++
		}
		if s.Prov.IsBootstrapAdmin(newEmail) && u.OrgRole != "admin" {
			if _, err := q.SetUserOrgRole(ctx, dbq.SetUserOrgRoleParams{ID: u.ID, OrgRole: "admin"}); err != nil {
				return err
			}
		}
		switch {
		case inactive && u.Active:
			delta["usersDeactivated"]++
			_, err = s.Prov.Deactivate(ctx, q, u.ID)
			return err
		case !inactive && (!u.Active || u.DeletedAt != nil):
			delta["usersReactivated"]++
			_, err = q.SetUserActive(ctx, dbq.SetUserActiveParams{ID: u.ID, Active: true})
			return err
		}
		return nil
	})
	if errors.Is(err, errSkipUser) {
		stats["usersSkipped"]++
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	for k, v := range delta {
		stats[k] += v
	}
	return id, nil
}

// linkIdentity records that the Keycloak user (subject at the sign-in provider) is this account.
func linkIdentity(ctx context.Context, tx pgx.Tx, userID uuid.UUID, provider, subject, email string) error {
	_, err := tx.Exec(ctx, `INSERT INTO user_identities (user_id, provider, subject, email) VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, subject) DO UPDATE SET user_id = excluded.user_id, email = excluded.email
		WHERE user_identities.user_id IS DISTINCT FROM excluded.user_id OR user_identities.email IS DISTINCT FROM excluded.email`,
		userID, provider, subject, nilIfEmpty(email))
	return err
}

// wantGroup applies WFM_KEYCLOAK_GROUP_PREFIX to a group's own name (not its path), ignoring case.
func (s *KeycloakSyncer) wantGroup(g KeycloakGroup) bool {
	return s.Cfg.GroupPrefix == "" || strings.HasPrefix(strings.ToLower(g.Name), strings.ToLower(s.Cfg.GroupPrefix))
}

// keycloakGroupName is a synced group's display name: its path without the leading "/". Paths
// are unique in a realm (names are not: two parents may each have a "backend" subgroup) and
// match what a groups claim with full paths carries at sign-in.
func keycloakGroupName(g KeycloakGroup) string {
	if p := strings.TrimPrefix(g.Path, "/"); p != "" {
		return p
	}
	return g.Name
}

func (s *KeycloakSyncer) syncGroups(ctx context.Context, local map[string]uuid.UUID, stats Stats) error {
	all, err := s.Client.ListGroups(ctx)
	if err != nil {
		return err
	}
	var groups []KeycloakGroup
	want := map[string]bool{}
	for _, g := range all {
		if s.wantGroup(g) {
			groups = append(groups, g)
			want[g.ID] = true
		}
	}
	// Remove groups deleted upstream (or no longer matching the prefix) first, so a group
	// re-created under the same path does not clash with its predecessor.
	existing, err := s.Q.ListGroupsBySource(ctx, "keycloak")
	if err != nil {
		return err
	}
	for _, g := range existing {
		if g.ExternalID == nil || !want[*g.ExternalID] {
			if err := s.Q.DeleteGroup(ctx, g.ID); err != nil {
				return err
			}
			stats["groupsDeleted"]++
		}
	}
	for _, kg := range groups {
		ids, err := s.Client.ListMemberIDs(ctx, kg.ID)
		if err != nil {
			return fmt.Errorf("group %s: %w", kg.Path, err)
		}
		members := map[uuid.UUID]bool{}
		for _, kid := range ids {
			if id, ok := local[kid]; ok {
				members[id] = true // people outside the access group have no account here
			}
		}
		delta := Stats{}
		err = db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
			name := keycloakGroupName(kg)
			g, err := q.GetGroupBySourceExternal(ctx, dbq.GetGroupBySourceExternalParams{Source: "keycloak", ExternalID: &kg.ID})
			switch {
			case db.IsNoRows(err):
				g, err = q.CreateGroup(ctx, dbq.CreateGroupParams{DisplayName: name, Description: kg.Description, Source: "keycloak", ExternalID: &kg.ID})
				if err != nil {
					return err
				}
				delta["groupsCreated"]++
			case err != nil:
				return err
			case g.DisplayName != name || g.Description != kg.Description:
				if g, err = q.UpdateGroup(ctx, dbq.UpdateGroupParams{ID: g.ID, DisplayName: name, Description: kg.Description, ExternalID: &kg.ID}); err != nil {
					return err
				}
				delta["groupsUpdated"]++
			}
			current, err := q.GroupMemberIDs(ctx, g.ID)
			if err != nil {
				return err
			}
			have := map[uuid.UUID]bool{}
			for _, id := range current {
				have[id] = true
				if !members[id] {
					if err := q.RemoveGroupMember(ctx, dbq.RemoveGroupMemberParams{GroupID: g.ID, UserID: id}); err != nil {
						return err
					}
					delta["membershipsRemoved"]++
				}
			}
			for id := range members {
				if !have[id] {
					if err := q.AddGroupMember(ctx, dbq.AddGroupMemberParams{GroupID: g.ID, UserID: id}); err != nil {
						return err
					}
					delta["membershipsAdded"]++
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("group %s: %w", kg.Path, err)
		}
		for k, v := range delta {
			stats[k] += v
		}
		stats["memberships"] += len(members)
	}
	stats["groups"] = len(groups)
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
