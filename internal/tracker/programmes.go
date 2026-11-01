package tracker

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db/dbq"
)

/*
 * Programmes: a project made from the programme template holds initiatives (level 2) that team
 * projects' epics work toward. Its admins choose the team projects; an epic in a team may then
 * have one of the programme's initiatives as its parent, the one parent link that crosses
 * projects (stories and sub-tasks stay with their epic, so boards and sprints are untouched).
 *
 * Membership grants nothing: every view of a programme shows only the teams, epics and issues the
 * viewer can browse, and an epic's initiative is shown only to people who can browse the
 * programme. Taking a team out of a programme unlinks its epics from the programme's initiatives,
 * so a parent in another project always means the child's project is one of its teams.
 */

// ProgrammeTeams is a programme's team projects the viewer can browse, and how many more there are.
type ProgrammeTeams struct {
	Programme dbq.Project
	Teams     []dbq.Project
	Hidden    int
}

// IDs is the programme and its visible teams: the projects a programme's views cover.
func (t ProgrammeTeams) IDs() []uuid.UUID {
	ids := []uuid.UUID{t.Programme.ID}
	for _, p := range t.Teams {
		ids = append(ids, p.ID)
	}
	return ids
}

// programmeFor resolves a programme the actor can browse, with perm; other projects read as not found.
func (s *Service) programmeFor(ctx context.Context, actor *authz.Actor, ref string, perm authz.Permission) (dbq.Project, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, perm)
	if err != nil {
		return p, err
	}
	if !IsProgramme(p) {
		return p, apperr.NotFound("programme")
	}
	return p, nil
}

// programmeTeams lists a programme's teams, split into those the actor can browse and a count of the rest.
func (s *Service) programmeTeams(ctx context.Context, q *dbq.Queries, actor *authz.Actor, prog dbq.Project) (ProgrammeTeams, error) {
	all, err := q.ProgrammeProjects(ctx, prog.ID)
	if err != nil {
		return ProgrammeTeams{}, err
	}
	out := ProgrammeTeams{Programme: prog, Teams: []dbq.Project{}}
	for _, p := range all {
		access, err := s.Authz.Access(ctx, actor, p)
		if err != nil {
			return out, err
		}
		if access.CanBrowse() {
			out.Teams = append(out.Teams, p)
		} else {
			out.Hidden++
		}
	}
	return out, nil
}

// ProgrammeTeams lists a programme's teams the actor can browse.
func (s *Service) ProgrammeTeams(ctx context.Context, actor *authz.Actor, ref string) (ProgrammeTeams, error) {
	prog, err := s.programmeFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return ProgrammeTeams{}, err
	}
	return s.programmeTeams(ctx, s.Q, actor, prog)
}

// AddProgrammeTeam adds a team project to a programme. It takes the programme's admin permission
// and browse access to the team, like choosing the projects a help centre's What's new shows: the
// team's own data stays as visible as it was.
func (s *Service) AddProgrammeTeam(ctx context.Context, actor *authz.Actor, ref, teamKey string) (ProgrammeTeams, error) {
	prog, err := s.programmeFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return ProgrammeTeams{}, err
	}
	team, _, err := s.ProjectFor(ctx, actor, strings.TrimSpace(teamKey), authz.ProjectBrowse)
	if apperr.IsNotFound(err) {
		return ProgrammeTeams{}, apperr.Invalid("projectKey", "no project %s", teamKey)
	}
	if err != nil {
		return ProgrammeTeams{}, err
	}
	switch {
	case team.ID == prog.ID:
		return ProgrammeTeams{}, apperr.Invalid("projectKey", "a programme can't be one of its own teams")
	case IsProgramme(team):
		return ProgrammeTeams{}, apperr.Invalid("projectKey", "%s is a programme; add the projects of the teams that work toward it", team.Key)
	case IsServiceProject(team):
		return ProgrammeTeams{}, apperr.Invalid("projectKey", "%s is a service project; add software projects", team.Key)
	}
	var out ProgrammeTeams
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.AddProgrammeProject(ctx, dbq.AddProgrammeProjectParams{ProgrammeID: prog.ID, ProjectID: team.ID, AddedBy: actor.UserID()}); err != nil {
			return err
		}
		data := map[string]any{"programme": prog.Key, "team": team.Key}
		Audit(ctx, q, actor, "programme.team_add", "project", prog.ID.String(), &prog.ID, data)
		Audit(ctx, q, actor, "programme.team_add", "project", team.ID.String(), &team.ID, data)
		if out, err = s.programmeTeams(ctx, q, actor, prog); err != nil {
			return err
		}
		return s.emit(ctx, tx, actor, prog, nil, "project.updated", nil)
	})
	return out, err
}

// RemoveProgrammeTeam takes a team project out of a programme, unlinking the team's epics from the
// programme's initiatives (each epic's history says so). It returns how many epics were unlinked.
func (s *Service) RemoveProgrammeTeam(ctx context.Context, actor *authz.Actor, ref, teamKey string) (int, error) {
	prog, err := s.programmeFor(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return 0, err
	}
	// The admin may not be able to browse the team (it shows as hidden); its key is enough to remove it.
	team, err := s.ResolveProject(ctx, s.Q, strings.TrimSpace(teamKey))
	if err != nil {
		return 0, apperr.NotFound("team")
	}
	unlinked := 0
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		before, err := q.ProgrammeCrossProjectChildren(ctx, prog.ID)
		if err != nil {
			return err
		}
		n, err := q.RemoveProgrammeProject(ctx, dbq.RemoveProgrammeProjectParams{ProgrammeID: prog.ID, ProjectID: team.ID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("team")
		}
		epics, err := q.UnlinkProgrammeEpics(ctx, dbq.UnlinkProgrammeEpicsParams{TeamID: team.ID, ProgrammeID: prog.ID})
		if err != nil {
			return err
		}
		parents := map[uuid.UUID]uuid.UUID{}
		for _, c := range before {
			if c.ParentID != nil {
				parents[c.ID] = *c.ParentID
			}
		}
		if err := s.recordUnlinked(ctx, tx, q, actor, prog, team, epics, parents); err != nil {
			return err
		}
		unlinked = len(epics)
		data := map[string]any{"programme": prog.Key, "team": team.Key, "unlinkedEpics": unlinked}
		Audit(ctx, q, actor, "programme.team_remove", "project", prog.ID.String(), &prog.ID, data)
		Audit(ctx, q, actor, "programme.team_remove", "project", team.ID.String(), &team.ID, data)
		return s.emit(ctx, tx, actor, prog, nil, "project.updated", nil)
	})
	return unlinked, err
}

// recordUnlinked writes "parent" history on epics that lost their initiative in prog (parents
// maps each epic to the initiative it had) and refreshes the team's open views.
func (s *Service) recordUnlinked(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, prog, team dbq.Project, epics []dbq.Issue, parents map[uuid.UUID]uuid.UUID) error {
	if len(epics) == 0 {
		return nil
	}
	var ids []uuid.UUID
	for _, e := range epics {
		if id, ok := parents[e.ID]; ok {
			ids = append(ids, id)
		}
	}
	inits, err := q.GetIssuesByIDs(ctx, ids)
	if err != nil {
		return err
	}
	keys := map[uuid.UUID]string{}
	for _, i := range inits {
		keys[i.ID] = issueKey(prog, i)
	}
	for _, e := range epics {
		var from, fromRef *string
		if id, ok := parents[e.ID]; ok {
			k, ref := keys[id], id.String()
			from, fromRef = &k, &ref
		}
		if err := s.history(ctx, q, e.ID, actor, "parent", from, nil, fromRef, nil); err != nil {
			return err
		}
		if err := publishOnly(ctx, tx, actor, team, &e); err != nil {
			return err
		}
	}
	return nil
}

// unlinkChildrenElsewhere records, before parents are deleted, that their children in other
// projects (an initiative's epics) lose them: the database sets parent_id to NULL on delete.
func (s *Service) unlinkChildrenElsewhere(ctx context.Context, tx pgx.Tx, q *dbq.Queries, actor *authz.Actor, prog dbq.Project, children []dbq.Issue) error {
	byTeam := map[uuid.UUID][]dbq.Issue{}
	parents := map[uuid.UUID]uuid.UUID{}
	for _, c := range children {
		byTeam[c.ProjectID] = append(byTeam[c.ProjectID], c)
		if c.ParentID != nil {
			parents[c.ID] = *c.ParentID
		}
	}
	for teamID, epics := range byTeam {
		team, err := q.GetProject(ctx, teamID)
		if err != nil {
			return err
		}
		if err := s.recordUnlinked(ctx, tx, q, actor, prog, team, epics, parents); err != nil {
			return err
		}
	}
	return nil
}

// ProgrammesOf lists the programmes a project is a team of, among those the actor can browse.
func (s *Service) ProgrammesOf(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.Project, error) {
	p, _, err := s.ProjectFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	all, err := s.Q.ProgrammesOfProject(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := []dbq.Project{}
	for _, prog := range all {
		access, err := s.Authz.Access(ctx, actor, prog)
		if err != nil {
			return nil, err
		}
		if access.CanBrowse() {
			out = append(out, prog)
		}
	}
	return out, nil
}

// crossProjectParent checks a parent in another project: only an epic may have one, and only an
// initiative of a programme its project belongs to, which actor can browse. A parent actor can't
// browse reads as not found; otherwise the error says what is wrong.
func (s *Service) crossProjectParent(ctx context.Context, q *dbq.Queries, actor *authz.Actor, p dbq.Project, child dbq.IssueType, parent dbq.Issue) error {
	pp, err := q.GetProject(ctx, parent.ProjectID)
	if err != nil {
		return err
	}
	parentKey := issueKey(pp, parent)
	access, err := s.Authz.Access(ctx, actor, pp)
	if err != nil {
		return err
	}
	if !access.CanBrowse() {
		return apperr.Invalid("parentKey", "parent issue %s not found", parentKey)
	}
	pt, err := q.GetIssueType(ctx, parent.TypeID)
	if err != nil {
		return err
	}
	if child.Level != 1 || pt.Level != 2 || pt.Kind != KindWork || !IsProgramme(pp) {
		if child.Level == 1 {
			return apperr.Invalid("parentKey", "an epic's parent in another project must be an initiative in a programme; %s is not", parentKey)
		}
		return apperr.Invalid("parentKey", "the parent must be in the same project (only epics can belong to a programme's initiatives)")
	}
	member, err := q.IsProgrammeMember(ctx, dbq.IsProgrammeMemberParams{ProgrammeID: pp.ID, ProjectID: p.ID})
	if err != nil {
		return err
	}
	if !member {
		return apperr.Invalid("parentKey", "%s isn't one of the teams of programme %s; a programme admin can add it in the programme's settings", p.Key, pp.Key)
	}
	return nil
}

// canLeaveParent refuses to move an issue away from a parent in another project that actor can't
// browse (an epic's initiative in a programme they can't see): they don't know what they'd undo.
func (s *Service) canLeaveParent(ctx context.Context, q *dbq.Queries, actor *authz.Actor, i dbq.Issue, newParentKey string) error {
	if i.ParentID == nil {
		return nil
	}
	cur, err := q.GetIssue(ctx, *i.ParentID)
	if err != nil || cur.ProjectID == i.ProjectID {
		return nil
	}
	pp, err := q.GetProject(ctx, cur.ProjectID)
	if err != nil {
		return err
	}
	if newParentKey != "" && (strings.EqualFold(newParentKey, issueKey(pp, cur)) || newParentKey == cur.ID.String()) {
		return nil
	}
	access, err := s.Authz.Access(ctx, actor, pp)
	if err != nil {
		return err
	}
	if !access.CanBrowse() {
		return apperr.Forbidden("this epic belongs to an initiative you can't see; ask someone in its programme to move it")
	}
	return nil
}
