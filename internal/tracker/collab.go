package tracker

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/events"
)

// publishOnly refreshes open boards without enqueuing a domain event.
func publishOnly(ctx context.Context, tx pgx.Tx, actor *authz.Actor, p dbq.Project, i *dbq.Issue) error {
	ev := events.Event{Type: "issue.updated", ProjectID: p.ID, ActorID: actor.UserID()}
	if i != nil {
		ev.IssueID = &i.ID
		ev.IssueKey = issueKey(p, *i)
	}
	return events.Publish(ctx, tx, ev)
}

// ---------------------------------------------------------------------------
// Comments

// ListComments returns an issue's comments, internal notes included (agents see everything;
// the portal reads only public comments, through the servicedesk package).
func (s *Service) ListComments(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.Comment, dbq.Project, authz.Access, error) {
	i, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, p, access, err
	}
	cs, err := s.Q.ListComments(ctx, i.ID)
	return cs, p, access, err
}

// Comment visibility on service requests.
const (
	// VisibilityPublic is a reply the customer sees (in the portal and by email).
	VisibilityPublic = "public"
	// VisibilityInternal is a note only agents see.
	VisibilityInternal = "internal"
)

// CommentInput is a comment to add. Visibility only applies to service projects: "public"
// replies to the customer, "internal" is a note for agents, and "" is internal for agents,
// automation and smart commits (so nothing reaches a customer by accident) and public in the
// portal. Software projects have no internal comments.
type CommentInput struct {
	Body       string
	Visibility string
}

// commentInternal decides whether a new comment is an internal note.
func commentInternal(p dbq.Project, actor *authz.Actor, visibility string) (bool, error) {
	switch visibility {
	case "", VisibilityPublic, VisibilityInternal:
	default:
		return false, apperr.Invalid("visibility", "visibility is public or internal")
	}
	if !IsServiceProject(p) {
		if visibility == VisibilityInternal {
			return false, apperr.Invalid("visibility", "internal notes are only for service projects")
		}
		return false, nil
	}
	if actor.Type == authz.ActorPortal {
		return false, nil
	}
	return visibility != VisibilityPublic, nil
}

// commentRecorded writes what a new comment was to a service request's history (agents can tell
// replies to the customer from internal notes) and to its domain event.
func (s *Service) commentRecorded(ctx context.Context, q *dbq.Queries, actor *authz.Actor, p dbq.Project, c dbq.Comment, data map[string]any) error {
	if !IsServiceProject(p) {
		return nil
	}
	label := "Reply to customer"
	switch {
	case c.Internal:
		label = "Internal note"
	case c.Source == "portal":
		label = "Reply from the portal"
	}
	data["visibility"] = CommentVisibility(c)
	ref := c.ID.String()
	return s.history(ctx, q, c.IssueID, actor, "comment", nil, &label, nil, &ref)
}

// CommentVisibility names a comment's visibility.
func CommentVisibility(c dbq.Comment) string {
	if c.Internal {
		return VisibilityInternal
	}
	return VisibilityPublic
}

func (s *Service) AddComment(ctx context.Context, actor *authz.Actor, ref, body string) (dbq.Comment, error) {
	return s.AddCommentWith(ctx, actor, ref, CommentInput{Body: body})
}

func (s *Service) AddCommentWith(ctx context.Context, actor *authz.Actor, ref string, in CommentInput) (dbq.Comment, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return dbq.Comment{}, apperr.Invalid("body", "comment cannot be empty")
	}
	i, p, _, err := s.IssueFor(ctx, actor, ref, authz.IssueComment)
	if err != nil {
		return dbq.Comment{}, err
	}
	internal, err := commentInternal(p, actor, in.Visibility)
	if err != nil {
		return dbq.Comment{}, err
	}
	var c dbq.Comment
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		c, err = q.CreateComment(ctx, dbq.CreateCommentParams{IssueID: i.ID, AuthorID: actor.UserID(), Body: body, Source: commentSource(actor), Via: via(actor), Internal: internal})
		if err != nil {
			return err
		}
		// Customers follow their requests in the portal, not as watchers.
		if uid := actor.UserID(); uid != nil && actor.Type != authz.ActorPortal {
			if err := q.AddWatcher(ctx, dbq.AddWatcherParams{IssueID: i.ID, UserID: *uid}); err != nil {
				return err
			}
		}
		data := map[string]any{"commentId": c.ID, "body": truncate(body, 500)}
		if err := s.commentRecorded(ctx, q, actor, p, c, data); err != nil {
			return err
		}
		return s.emit(ctx, tx, actor, p, &i, "comment.created", data)
	})
	return c, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (s *Service) commentForEdit(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Comment, dbq.Issue, dbq.Project, error) {
	c, err := s.Q.GetComment(ctx, id)
	if err != nil {
		return c, dbq.Issue{}, dbq.Project{}, apperr.NotFound("comment")
	}
	i, p, access, err := s.IssueFor(ctx, actor, c.IssueID.String(), authz.ProjectBrowse)
	if err != nil {
		return c, i, p, err
	}
	own := c.AuthorID != nil && actor.UserID() != nil && *c.AuthorID == *actor.UserID()
	if !own && !access.Has(authz.CommentModerate) {
		return c, i, p, apperr.Forbidden("you can only change your own comments")
	}
	return c, i, p, nil
}

func (s *Service) UpdateComment(ctx context.Context, actor *authz.Actor, id uuid.UUID, body string) (dbq.Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return dbq.Comment{}, apperr.Invalid("body", "comment cannot be empty")
	}
	_, i, p, err := s.commentForEdit(ctx, actor, id)
	if err != nil {
		return dbq.Comment{}, err
	}
	var c dbq.Comment
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		if c, err = q.UpdateComment(ctx, dbq.UpdateCommentParams{ID: id, Body: body}); err != nil {
			return err
		}
		return publishOnly(ctx, tx, actor, p, &i)
	})
	return c, err
}

func (s *Service) DeleteComment(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	_, i, p, err := s.commentForEdit(ctx, actor, id)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DeleteComment(ctx, id); err != nil {
			return err
		}
		return publishOnly(ctx, tx, actor, p, &i)
	})
}

// ---------------------------------------------------------------------------
// History

func (s *Service) History(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.IssueHistory, error) {
	i, _, _, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	return s.Q.ListHistory(ctx, i.ID)
}

// ---------------------------------------------------------------------------
// Links

var linkLabels = map[string][2]string{
	"blocks":     {"blocks", "is blocked by"},
	"relates":    {"relates to", "relates to"},
	"duplicates": {"duplicates", "is duplicated by"},
	"clones":     {"clones", "is cloned by"},
	"mitigates":  {"mitigates", "is mitigated by"},
	"implements": {"implements", "is implemented by"},
	"supersedes": {"supersedes", "is superseded by"},
}

func LinkLabel(linkType string, outward bool) string {
	if outward {
		return linkLabels[linkType][0]
	}
	return linkLabels[linkType][1]
}

func (s *Service) CreateLink(ctx context.Context, actor *authz.Actor, ref, linkType, targetRef string, inward bool) (dbq.IssueLink, dbq.Issue, error) {
	if _, ok := linkLabels[linkType]; !ok {
		return dbq.IssueLink{}, dbq.Issue{}, apperr.Invalid("type", "unknown link type %q", linkType)
	}
	src, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return dbq.IssueLink{}, src, err
	}
	if !canEdit(access, actor, src) {
		return dbq.IssueLink{}, src, apperr.Forbidden("you cannot edit %s", issueKey(p, src))
	}
	target, tp, _, err := s.IssueFor(ctx, actor, targetRef, authz.ProjectBrowse)
	if err != nil {
		return dbq.IssueLink{}, src, apperr.Invalid("targetKey", "issue %s not found", targetRef)
	}
	if target.ID == src.ID {
		return dbq.IssueLink{}, src, apperr.Invalid("targetKey", "an issue cannot link to itself")
	}
	from, to := src, target
	if inward {
		from, to = target, src
	}
	if err := checkLinkKinds(ctx, s.Q, linkType, from, to); err != nil {
		return dbq.IssueLink{}, src, err
	}
	var l dbq.IssueLink
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		l, err = q.CreateIssueLink(ctx, dbq.CreateIssueLinkParams{SourceID: from.ID, TargetID: to.ID, LinkType: linkType, CreatedBy: actor.UserID()})
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperr.Conflict("duplicate_link", "those issues are already linked that way")
			}
			return err
		}
		label := fmt.Sprintf("%s %s", LinkLabel(linkType, !inward), issueKey(tp, target))
		if err := s.history(ctx, q, src.ID, actor, "link", nil, &label, nil, nil); err != nil {
			return err
		}
		if err := publishOnly(ctx, tx, actor, tp, &target); err != nil {
			return err
		}
		return s.emit(ctx, tx, actor, p, &src, "issue.updated", map[string]any{"fields": []string{"link"}})
	})
	return l, target, err
}

func (s *Service) DeleteLink(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	l, err := s.Q.GetIssueLink(ctx, id)
	if err != nil {
		return apperr.NotFound("link")
	}
	// A link is made from either end (CreateLink checks the issue it's made from), so whoever can
	// edit either end may remove it. The removal is recorded on the source, as it was added there.
	seen := false
	allowed := false
	for _, end := range []uuid.UUID{l.SourceID, l.TargetID} {
		i, _, access, err := s.IssueFor(ctx, actor, end.String(), authz.ProjectBrowse)
		if err != nil {
			continue
		}
		seen = true
		if canEdit(access, actor, i) {
			allowed = true
			break
		}
	}
	switch {
	case !seen:
		return apperr.NotFound("link")
	case !allowed:
		return apperr.Forbidden("you cannot edit either issue of this link")
	}
	src, err := s.Q.GetIssue(ctx, l.SourceID)
	if err != nil {
		return err
	}
	p, err := s.Q.GetProject(ctx, src.ProjectID)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DeleteIssueLink(ctx, id); err != nil {
			return err
		}
		target, _ := q.GetIssue(ctx, l.TargetID)
		tp, _ := q.GetProject(ctx, target.ProjectID)
		label := fmt.Sprintf("%s %s", LinkLabel(l.LinkType, true), issueKey(tp, target))
		if err := s.history(ctx, q, src.ID, actor, "link", &label, nil, nil, nil); err != nil {
			return err
		}
		return publishOnly(ctx, tx, actor, p, &src)
	})
}

// ---------------------------------------------------------------------------
// Watchers

func (s *Service) Watchers(ctx context.Context, actor *authz.Actor, ref string) ([]dbq.User, error) {
	i, _, _, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return nil, err
	}
	return s.Q.ListWatchers(ctx, i.ID)
}

func (s *Service) AddWatcher(ctx context.Context, actor *authz.Actor, ref string, userID *uuid.UUID) error {
	i, p, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return err
	}
	target := actor.UserID()
	if userID != nil && (target == nil || *userID != *target) {
		if !access.Has(authz.IssueEditOthers) {
			return apperr.Forbidden("you can only add yourself as a watcher")
		}
		if _, err := s.assignable(ctx, s.Q, p, *userID); err != nil {
			return apperr.Invalid("userId", "that user cannot access %s", p.Key)
		}
		target = userID
	}
	if target == nil {
		return apperr.BadRequest("no user to add")
	}
	return s.Q.AddWatcher(ctx, dbq.AddWatcherParams{IssueID: i.ID, UserID: *target})
}

func (s *Service) RemoveWatcher(ctx context.Context, actor *authz.Actor, ref string, userID uuid.UUID) error {
	i, _, access, err := s.IssueFor(ctx, actor, ref, authz.ProjectBrowse)
	if err != nil {
		return err
	}
	if uid := actor.UserID(); (uid == nil || *uid != userID) && !access.Has(authz.IssueEditOthers) {
		return apperr.Forbidden("you can only remove yourself as a watcher")
	}
	return s.Q.RemoveWatcher(ctx, dbq.RemoveWatcherParams{IssueID: i.ID, UserID: userID})
}

// ---------------------------------------------------------------------------
// Attachments

func (s *Service) Attach(ctx context.Context, actor *authz.Actor, ref, filename, contentType string, size int64, r io.Reader) (dbq.Attachment, error) {
	i, p, _, err := s.IssueFor(ctx, actor, ref, authz.IssueComment)
	if err != nil {
		return dbq.Attachment{}, err
	}
	if size > s.Cfg.AttachmentMaxBytes {
		return dbq.Attachment{}, apperr.TooLarge("attachments are limited to %d MB", s.Cfg.AttachmentMaxBytes/1024/1024)
	}
	filename = path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if filename == "." || filename == "/" || filename == "" {
		filename = "attachment"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	key := fmt.Sprintf("attachments/%s/%s/%s", p.ID, i.ID, uuid.NewString())
	// Read at most the limit + 1 byte so an understated size cannot bypass the cap.
	limited := io.LimitReader(r, s.Cfg.AttachmentMaxBytes+1)
	counter := &countingReader{r: limited}
	if err := s.Storage.Put(ctx, key, counter, size, contentType); err != nil {
		return dbq.Attachment{}, fmt.Errorf("store attachment: %w", err)
	}
	if counter.n > s.Cfg.AttachmentMaxBytes {
		_ = s.Storage.Delete(ctx, key)
		return dbq.Attachment{}, apperr.TooLarge("attachments are limited to %d MB", s.Cfg.AttachmentMaxBytes/1024/1024)
	}
	var a dbq.Attachment
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		var err error
		a, err = q.CreateAttachment(ctx, dbq.CreateAttachmentParams{
			IssueID: i.ID, UploaderID: actor.UserID(), Filename: filename, ContentType: contentType, SizeBytes: counter.n, ObjectKey: key,
			// Only files customers add in the portal are shown there.
			Public: actor.Type == authz.ActorPortal,
		})
		if err != nil {
			return err
		}
		if err := s.history(ctx, q, i.ID, actor, "attachment", nil, &filename, nil, nil); err != nil {
			return err
		}
		return publishOnly(ctx, tx, actor, p, &i)
	})
	if err != nil {
		_ = s.Storage.Delete(ctx, key)
	}
	return a, err
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (s *Service) OpenAttachment(ctx context.Context, actor *authz.Actor, id uuid.UUID) (dbq.Attachment, io.ReadCloser, *url.URL, error) {
	a, err := s.Q.GetAttachment(ctx, id)
	if err != nil {
		return a, nil, nil, apperr.NotFound("attachment")
	}
	if _, _, _, err := s.IssueFor(ctx, actor, a.IssueID.String(), authz.ProjectBrowse); err != nil {
		return a, nil, nil, apperr.NotFound("attachment")
	}
	rc, u, err := s.Storage.Open(ctx, a.ObjectKey, a.Filename)
	return a, rc, u, err
}

func (s *Service) DeleteAttachment(ctx context.Context, actor *authz.Actor, id uuid.UUID) error {
	a, err := s.Q.GetAttachment(ctx, id)
	if err != nil {
		return apperr.NotFound("attachment")
	}
	i, p, access, err := s.IssueFor(ctx, actor, a.IssueID.String(), authz.ProjectBrowse)
	if err != nil {
		return apperr.NotFound("attachment")
	}
	own := a.UploaderID != nil && actor.UserID() != nil && *a.UploaderID == *actor.UserID()
	if !own && !access.Has(authz.CommentModerate) {
		return apperr.Forbidden("you can only delete your own attachments")
	}
	err = s.tx(ctx, func(tx pgx.Tx, q *dbq.Queries) error {
		if err := q.DeleteAttachment(ctx, id); err != nil {
			return err
		}
		if err := s.history(ctx, q, i.ID, actor, "attachment", &a.Filename, nil, nil, nil); err != nil {
			return err
		}
		return publishOnly(ctx, tx, actor, p, &i)
	})
	if err == nil {
		if derr := s.Storage.Delete(ctx, a.ObjectKey); derr != nil {
			s.Log.Warn("attachment content not deleted", "key", a.ObjectKey, "error", derr)
		}
	}
	return err
}
