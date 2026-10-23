package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/identity"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

type Syncer struct {
	Pool *pgxpool.Pool
	Q    *dbq.Queries
	Cfg  config.GoogleDirectory
	Prov *identity.Provisioner
	Log  *slog.Logger
	// NewClient is overridable for tests.
	NewClient func(ctx context.Context) (Client, error)
}

func NewSyncer(pool *pgxpool.Pool, cfg config.GoogleDirectory, prov *identity.Provisioner, log *slog.Logger) *Syncer {
	s := &Syncer{Pool: pool, Q: dbq.New(pool), Cfg: cfg, Prov: prov, Log: log}
	s.NewClient = func(ctx context.Context) (Client, error) { return NewClient(ctx, cfg) }
	return s
}

type Stats map[string]int

// Run performs one sync and records it in directory_sync_runs.
func (s *Syncer) Run(ctx context.Context, trigger string) (Stats, error) {
	run, err := s.Q.CreateDirectorySyncRun(ctx, trigger)
	if err != nil {
		return nil, err
	}
	stats, runErr := s.sync(ctx)
	status := "succeeded"
	var errMsg *string
	if runErr != nil {
		status = "failed"
		m := runErr.Error()
		errMsg = &m
	}
	b, _ := json.Marshal(stats)
	if err := s.Q.FinishDirectorySyncRun(context.WithoutCancel(ctx), dbq.FinishDirectorySyncRunParams{ID: run.ID, Status: status, Stats: b, Error: errMsg}); err != nil {
		return stats, err
	}
	data := map[string]any{"trigger": trigger, "status": status}
	for k, v := range stats {
		data[k] = v
	}
	tracker.Audit(ctx, s.Q, authz.SystemActor(), "directory.sync", "directory", run.ID.String(), nil, data)
	return stats, runErr
}

func (s *Syncer) sync(ctx context.Context) (Stats, error) {
	stats := Stats{}
	client, err := s.NewClient(ctx)
	if err != nil {
		return stats, err
	}
	all, err := client.ListUsers(ctx, "")
	if err != nil {
		return stats, err
	}
	byID := map[string]User{}
	for _, u := range all {
		byID[u.ID] = u
	}
	inScope, err := s.scope(ctx, client, all)
	if err != nil {
		return stats, err
	}

	seen := map[uuid.UUID]bool{}
	for _, gu := range all {
		if !inScope[gu.ID] {
			continue
		}
		id, err := s.upsertUser(ctx, gu, stats)
		if err != nil {
			return stats, fmt.Errorf("user %s: %w", gu.PrimaryEmail, err)
		}
		if id != uuid.Nil {
			seen[id] = true
		}
	}

	// Directory-provisioned users that are out of scope or gone upstream.
	rows, err := s.Pool.Query(ctx, `SELECT id, google_id FROM users WHERE source = 'google' AND deleted_at IS NULL`)
	if err != nil {
		return stats, err
	}
	type local struct {
		id       uuid.UUID
		googleID *string
	}
	locals, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (local, error) {
		var l local
		return l, r.Scan(&l.id, &l.googleID)
	})
	if err != nil {
		return stats, err
	}
	for _, l := range locals {
		if seen[l.id] {
			continue
		}
		err := db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
			if l.googleID != nil {
				if _, exists := byID[*l.googleID]; exists {
					u, err := q.GetUser(ctx, l.id)
					if err != nil || !u.Active {
						return err
					}
					stats["usersDeactivated"]++
					_, err = s.Prov.Deactivate(ctx, q, l.id) // removed from the access group / OU
					return err
				}
			}
			stats["usersDeleted"]++
			return s.Prov.MarkDeleted(ctx, q, l.id) // deleted in Google
		})
		if err != nil {
			return stats, err
		}
	}

	if s.Cfg.SyncGroups {
		if err := s.syncGroups(ctx, client, stats); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// scope returns the Google user ids that should have access: members of the configured group
// and/or users in the configured org unit; everyone when neither is set.
func (s *Syncer) scope(ctx context.Context, client Client, all []User) (map[string]bool, error) {
	in := map[string]bool{}
	if s.Cfg.Group == "" && s.Cfg.OrgUnit == "" {
		for _, u := range all {
			in[u.ID] = true
		}
		return in, nil
	}
	if s.Cfg.Group != "" {
		members, err := client.ListMembers(ctx, s.Cfg.Group)
		if err != nil {
			return nil, fmt.Errorf("access group %s: %w", s.Cfg.Group, err)
		}
		for _, m := range members {
			if m.Type == "USER" {
				in[m.ID] = true
			}
		}
	}
	if s.Cfg.OrgUnit != "" {
		ou := strings.TrimRight(s.Cfg.OrgUnit, "/")
		for _, u := range all {
			if u.OrgUnitPath == ou || strings.HasPrefix(u.OrgUnitPath, ou+"/") || ou == "" {
				in[u.ID] = true
			}
		}
	}
	return in, nil
}

func (s *Syncer) upsertUser(ctx context.Context, gu User, stats Stats) (uuid.UUID, error) {
	email := strings.ToLower(gu.PrimaryEmail)
	if email == "" {
		return uuid.Nil, nil
	}
	inactive := gu.Suspended || gu.Archived
	name := gu.Name.FullName
	if name == "" {
		name = strings.TrimSpace(gu.Name.GivenName + " " + gu.Name.FamilyName)
	}
	if name == "" {
		name = email
	}
	var id uuid.UUID
	err := db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		u, err := q.GetUserByGoogleID(ctx, &gu.ID)
		if db.IsNoRows(err) {
			u, err = q.GetUserByEmail(ctx, email)
		}
		switch {
		case db.IsNoRows(err):
			if inactive {
				return nil // never create accounts for suspended people
			}
			role := "member"
			if s.Prov.IsBootstrapAdmin(email) {
				role = "admin"
			}
			u, err = q.CreateUser(ctx, dbq.CreateUserParams{
				Kind: "human", Email: email, Username: email, DisplayName: name, GivenName: nilIfEmpty(gu.Name.GivenName),
				FamilyName: nilIfEmpty(gu.Name.FamilyName), AvatarUrl: nilIfEmpty(gu.ThumbnailPhotoURL), OrgRole: role,
				Source: "google", GoogleID: &gu.ID, Active: true,
			})
			if err != nil {
				return err
			}
			stats["usersCreated"]++
			id = u.ID
			return nil
		case err != nil:
			return err
		}
		id = u.ID
		if u.DeletedAt != nil && u.AnonymizedAt != nil {
			return nil
		}
		if u.GoogleID == nil {
			if err := q.LinkGoogleID(ctx, dbq.LinkGoogleIDParams{ID: u.ID, GoogleID: &gu.ID}); err != nil && !db.IsUniqueViolation(err) {
				return err
			}
		}
		// SCIM-managed accounts belong to their IdP; only link them.
		if u.Source == "scim" {
			return nil
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE users SET source = 'google', email = $2, username = CASE WHEN username = email THEN $2 ELSE username END,
				display_name = $3, given_name = $4, family_name = $5, avatar_url = coalesce($6, avatar_url), deleted_at = NULL, updated_at = now()
			WHERE id = $1`, u.ID, email, name, nilIfEmpty(gu.Name.GivenName), nilIfEmpty(gu.Name.FamilyName), nilIfEmpty(gu.ThumbnailPhotoURL)); err != nil {
			return err
		}
		stats["usersUpdated"]++
		switch {
		case inactive && u.Active:
			stats["usersDeactivated"]++
			_, err = s.Prov.Deactivate(ctx, q, u.ID)
			return err
		case !inactive && (!u.Active || u.DeletedAt != nil):
			stats["usersReactivated"]++
			_, err = q.SetUserActive(ctx, dbq.SetUserActiveParams{ID: u.ID, Active: true})
			return err
		}
		return nil
	})
	return id, err
}

func (s *Syncer) syncGroups(ctx context.Context, client Client, stats Stats) error {
	groups, err := client.ListGroups(ctx)
	if err != nil {
		return err
	}
	keep := map[uuid.UUID]bool{}
	for _, gg := range groups {
		if s.Cfg.GroupPrefix != "" && !strings.HasPrefix(strings.ToLower(gg.Email), s.Cfg.GroupPrefix) {
			continue
		}
		members, err := client.ListMembers(ctx, gg.ID)
		if err != nil {
			return fmt.Errorf("group %s: %w", gg.Email, err)
		}
		err = db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
			g, err := q.GetGroupBySourceExternal(ctx, dbq.GetGroupBySourceExternalParams{Source: "google", ExternalID: &gg.ID})
			name := gg.Name
			if name == "" {
				name = gg.Email
			}
			email := strings.ToLower(gg.Email)
			switch {
			case db.IsNoRows(err):
				g, err = q.CreateGroup(ctx, dbq.CreateGroupParams{DisplayName: name, Description: gg.Description, Source: "google", ExternalID: &gg.ID, Email: &email})
				if err != nil {
					return err
				}
				stats["groupsCreated"]++
			case err != nil:
				return err
			default:
				if g, err = q.UpdateGroup(ctx, dbq.UpdateGroupParams{ID: g.ID, DisplayName: name, Description: gg.Description, ExternalID: &gg.ID, Email: &email}); err != nil {
					return err
				}
				stats["groupsUpdated"]++
			}
			keep[g.ID] = true
			if err := q.ClearGroupMembers(ctx, g.ID); err != nil {
				return err
			}
			for _, m := range members {
				if m.Type != "USER" {
					continue
				}
				u, err := q.GetUserByGoogleID(ctx, &m.ID)
				if db.IsNoRows(err) {
					u, err = q.GetUserByEmail(ctx, m.Email)
				}
				if err != nil {
					continue // not provisioned here
				}
				if err := q.AddGroupMember(ctx, dbq.AddGroupMemberParams{GroupID: g.ID, UserID: u.ID}); err != nil {
					return err
				}
				stats["memberships"]++
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	existing, err := s.Q.ListGroupsBySource(ctx, "google")
	if err != nil {
		return err
	}
	for _, g := range existing {
		if !keep[g.ID] {
			if err := s.Q.DeleteGroup(ctx, g.ID); err != nil {
				return err
			}
			stats["groupsDeleted"]++
		}
	}
	return nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
