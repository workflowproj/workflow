package servicedesk

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/search"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

// queueOrder sorts a queue without an ORDER BY: oldest first, like a line.
var queueOrder = []search.Order{{Field: "created"}}

func (s *Service) queueView(ctx context.Context, actor *authz.Actor, p dbq.Project, q dbq.ServiceQueue) api.Queue {
	v := api.Queue{Id: q.ID, Name: q.Name, Jql: q.Jql, Position: int(q.Position)}
	_, total, err := s.Tracker.SearchWith(ctx, actor, []uuid.UUID{p.ID}, tracker.SearchQuery{JQL: q.Jql, DefaultOrder: queueOrder, Limit: 1})
	if err != nil {
		s.Log.Warn("queue count", "queue", q.ID, "error", err)
	}
	v.Count = total
	return v
}

// ListQueues lists a desk's queues with the number of requests in each (agents).
func (s *Service) ListQueues(ctx context.Context, actor *authz.Actor, ref string) ([]api.Queue, error) {
	p, _, _, err := s.desk(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	qs, err := s.Q.ListQueues(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := []api.Queue{}
	for _, q := range qs {
		out = append(out, s.queueView(ctx, actor, p, q))
	}
	return out, nil
}

func (s *Service) validateQueue(ctx context.Context, actor *authz.Actor, in api.QueueInput) (string, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 80 {
		return "", "", apperr.Invalid("name", "a queue needs a name of at most 80 characters")
	}
	jql := strings.TrimSpace(in.Jql)
	if err := s.Tracker.ValidateJQL(ctx, actor, jql); err != nil {
		return "", "", err
	}
	return name, jql, nil
}

func (s *Service) CreateQueue(ctx context.Context, actor *authz.Actor, ref string, in api.QueueInput) (api.Queue, error) {
	p, _, _, err := s.desk(ctx, actor, ref, authz.ProjectAdmin)
	if err != nil {
		return api.Queue{}, err
	}
	name, jql, err := s.validateQueue(ctx, actor, in)
	if err != nil {
		return api.Queue{}, err
	}
	pos := deref(in.Position)
	if in.Position == nil {
		qs, err := s.Q.ListQueues(ctx, p.ID)
		if err != nil {
			return api.Queue{}, err
		}
		pos = len(qs)
	}
	q, err := s.Q.CreateQueue(ctx, dbq.CreateQueueParams{ProjectID: p.ID, Name: name, Jql: jql, Position: int32(pos)})
	if err != nil {
		return api.Queue{}, err
	}
	return s.queueView(ctx, actor, p, q), nil
}

// queueFor loads a queue for an agent with perm in its project.
func (s *Service) queueFor(ctx context.Context, actor *authz.Actor, id uuid.UUID, perm authz.Permission) (dbq.ServiceQueue, dbq.Project, error) {
	q, err := s.Q.GetQueue(ctx, id)
	if err != nil {
		return q, dbq.Project{}, apperr.NotFound("queue")
	}
	p, _, _, err := s.desk(ctx, actor, q.ProjectID.String(), perm)
	if err != nil && apperr.IsNotFound(err) {
		return q, p, apperr.NotFound("queue")
	}
	return q, p, err
}

func (s *Service) UpdateQueue(ctx context.Context, actor *authz.Actor, id uuid.UUID, in api.QueueInput) (api.Queue, error) {
	q, p, err := s.queueFor(ctx, actor, id, authz.ProjectAdmin)
	if err != nil {
		return api.Queue{}, err
	}
	name, jql, err := s.validateQueue(ctx, actor, in)
	if err != nil {
		return api.Queue{}, err
	}
	pos := q.Position
	if in.Position != nil {
		pos = int32(*in.Position)
	}
	q, err = s.Q.UpdateQueue(ctx, dbq.UpdateQueueParams{ID: q.ID, Name: name, Jql: jql, Position: pos})
	if err != nil {
		return api.Queue{}, err
	}
	return s.queueView(ctx, actor, p, q), nil
}

func (s *Service) DeleteQueue(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	q, _, err := s.queueFor(ctx, actor, id, authz.ProjectAdmin)
	if err != nil {
		return err
	}
	return s.Q.DeleteQueue(ctx, q.ID)
}

// QueueIssues lists a queue's requests with their request types and SLA clocks.
func (s *Service) QueueIssues(ctx context.Context, actor *authz.Actor, id uuid.UUID, limit, offset int) (api.QueueIssues, error) {
	q, p, err := s.queueFor(ctx, actor, id, authz.ProjectBrowse)
	if err != nil {
		return api.QueueIssues{}, err
	}
	issues, total, err := s.Tracker.SearchWith(ctx, actor, []uuid.UUID{p.ID}, tracker.SearchQuery{JQL: q.Jql, DefaultOrder: queueOrder, Limit: limit, Offset: offset})
	if err != nil {
		return api.QueueIssues{}, err
	}
	views, err := s.Tracker.IssueViews(ctx, issues)
	if err != nil {
		return api.QueueIssues{}, err
	}
	ids := make([]uuid.UUID, len(issues))
	for k, i := range issues {
		ids[k] = i.ID
	}
	slas, err := s.Q.IssueSLAsFor(ctx, ids)
	if err != nil {
		return api.QueueIssues{}, err
	}
	srs, err := s.Q.ServiceRequestsFor(ctx, ids)
	if err != nil {
		return api.QueueIssues{}, err
	}
	rts, err := s.Q.ListRequestTypes(ctx, p.ID)
	if err != nil {
		return api.QueueIssues{}, err
	}
	out := api.QueueIssues{Queue: api.Queue{Id: q.ID, Name: q.Name, Jql: q.Jql, Position: int(q.Position), Count: total}, Total: total, Items: []api.QueueItem{}}
	for k, i := range issues {
		item := api.QueueItem{Issue: views[k]}
		var mine []dbq.IssueSla
		for _, r := range slas {
			if r.IssueID == i.ID {
				mine = append(mine, r)
			}
		}
		item.Slas = s.slaViews(ctx, mine)
		for _, sr := range srs {
			if sr.IssueID == i.ID && sr.RequestTypeID != nil {
				for _, rt := range rts {
					if rt.ID == *sr.RequestTypeID {
						ref := requestTypeRef(rt)
						item.RequestType = &ref
					}
				}
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
