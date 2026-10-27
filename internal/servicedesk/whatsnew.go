package servicedesk

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/workflowproj/workflow/internal/api"
	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/tracker"
)

/*
 * What's new: a help centre can show the published release notes of software projects its admins
 * chose. Two decisions are needed before customers read anything: the software project publishes
 * a release's notes (release.manage there), and the desk's admin, who must be able to browse that
 * project, adds it to the desk. Customers get the notes' text and the product's and release's
 * names and dates only, never issues.
 */

// maxWhatsNew caps the release notes the What's new page lists.
const maxWhatsNew = 100

// releaseNotesProjects lists the projects whose release notes a desk shows.
func (s *Service) releaseNotesProjects(ctx context.Context, q *dbq.Queries, deskID uuid.UUID) ([]api.ProjectRef, error) {
	ps, err := q.DeskReleaseProjects(ctx, deskID)
	if err != nil {
		return nil, err
	}
	out := make([]api.ProjectRef, 0, len(ps))
	for _, p := range ps {
		out = append(out, api.ProjectRef{Id: p.ID, Key: p.Key, Name: p.Name})
	}
	return out, nil
}

// setReleaseNotesProjects replaces the projects a desk shows release notes of. Only software
// projects qualify, and the admin must be able to browse each one they add; projects already
// chosen (perhaps by another admin) can stay or go without that.
func (s *Service) setReleaseNotesProjects(ctx context.Context, actor *authz.Actor, desk dbq.Project, keys []string) error {
	current, err := s.Q.DeskReleaseProjects(ctx, desk.ID)
	if err != nil {
		return err
	}
	chosen := map[string]uuid.UUID{}
	for _, p := range current {
		chosen[strings.ToUpper(p.Key)] = p.ID
	}
	var ids []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if id, ok := chosen[strings.ToUpper(key)]; ok {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			continue
		}
		p, _, err := s.Tracker.ProjectFor(ctx, actor, key, authz.ProjectBrowse)
		if apperr.IsNotFound(err) {
			return apperr.Invalid("releaseNotesProjectKeys", "no project %s", key)
		}
		if err != nil {
			return err
		}
		if tracker.IsServiceProject(p) {
			return apperr.Invalid("releaseNotesProjectKeys", "%s is a service project; choose software projects", p.Key)
		}
		if !seen[p.ID] {
			seen[p.ID] = true
			ids = append(ids, p.ID)
		}
	}
	return s.tx(ctx, func(_ pgx.Tx, q *dbq.Queries) error {
		if err := q.ClearDeskReleaseProjects(ctx, desk.ID); err != nil {
			return err
		}
		for _, id := range ids {
			if err := q.AddDeskReleaseProject(ctx, dbq.AddDeskReleaseProjectParams{DeskProjectID: desk.ID, ProjectID: id, AddedBy: actor.UserID()}); err != nil {
				return err
			}
		}
		return nil
	})
}

// WhatsNew lists the published release notes a desk shows, newest first, to anyone who may raise
// requests in it ("not found" for everyone else).
func (s *Service) WhatsNew(ctx context.Context, actor *authz.Actor, ref string) ([]api.PortalReleaseNote, error) {
	_, p, _, err := s.portalDesk(ctx, actor, ref)
	if err != nil {
		return nil, err
	}
	rows, err := s.Q.PublishedReleaseNotes(ctx, dbq.PublishedReleaseNotesParams{DeskProjectID: p.ID, Lim: maxWhatsNew})
	if err != nil {
		return nil, err
	}
	out := make([]api.PortalReleaseNote, 0, len(rows))
	for _, r := range rows {
		n := api.PortalReleaseNote{Id: r.ID, Product: r.ProjectName, Name: r.Name, Notes: r.Notes}
		switch {
		case r.ReleasedAt != nil:
			n.Date = &openapi_types.Date{Time: *r.ReleasedAt}
		case r.ReleaseDate != nil:
			n.Date = &openapi_types.Date{Time: *r.ReleaseDate}
		}
		out = append(out, n)
	}
	return out, nil
}
