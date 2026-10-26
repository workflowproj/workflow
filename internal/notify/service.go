package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	netmail "net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/config"
	"github.com/workflowproj/workflow/internal/db"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/jobs"
	"github.com/workflowproj/workflow/internal/mail"
	"github.com/workflowproj/workflow/internal/secure"
)

// Service sends notification emails and manages email settings.
type Service struct {
	Pool   *pgxpool.Pool
	Q      *dbq.Queries
	Jobs   jobs.Enqueuer
	Mailer mail.Sender
	Box    *secure.Box
	Cfg    *config.Config
	Log    *slog.Logger
	Now    func() time.Time
}

func New(pool *pgxpool.Pool, enq jobs.Enqueuer, mailer mail.Sender, box *secure.Box, cfg *config.Config, log *slog.Logger) *Service {
	return &Service{Pool: pool, Q: dbq.New(pool), Jobs: enq, Mailer: mailer, Box: box, Cfg: cfg, Log: log, Now: time.Now}
}

// Limits that keep digests readable.
const (
	digestMaxIssues = 25
	digestMaxEvents = 8
	excerptLength   = 600
)

// Brand is the name emails are signed with: the display name of WFM_SMTP_FROM.
func (s *Service) Brand() string {
	if a, err := netmail.ParseAddress(s.Cfg.SMTP.From); err == nil && strings.TrimSpace(a.Name) != "" {
		return strings.TrimSpace(a.Name)
	}
	return "Workflow"
}

// Transport says how mail leaves: "smtp", or "memory" when no SMTP server is configured.
func (s *Service) Transport() string {
	if s.Cfg.SMTP.Delivers() {
		return "smtp"
	}
	return "memory"
}

func (s *Service) issueURL(key string) string {
	return s.Cfg.PublicURL + "/browse/" + url.PathEscape(key)
}

// ManageURL is where people change their email notification settings.
func (s *Service) ManageURL() string { return s.Cfg.PublicURL + "/profile#email-notifications" }

// ---------------------------------------------------------------------------
// Settings

func getSettings(ctx context.Context, q *dbq.Queries, userID uuid.UUID) (Settings, error) {
	r, err := q.GetEmailSettings(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Defaults(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return FromRow(r), nil
}

// Get returns a person's settings (the defaults until they change something).
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Settings, error) {
	return getSettings(ctx, s.Q, userID)
}

// Patch changes some settings; nil and missing entries stay as they are.
type Patch struct {
	Deliveries map[string]string
	DigestHour *int
	TimeZone   *string
	Paused     *bool
}

// Update validates and saves a patch.
func (s *Service) Update(ctx context.Context, userID uuid.UUID, p Patch) (Settings, error) {
	var out Settings
	err := db.Tx(ctx, s.Pool, func(_ pgx.Tx, q *dbq.Queries) error {
		cur, err := getSettings(ctx, q, userID)
		if err != nil {
			return err
		}
		for kind, d := range p.Deliveries {
			k, ok := KindByKey(kind)
			if !ok {
				return apperr.Invalid("preferences", "unknown notification kind %q", kind)
			}
			if !ValidDelivery(d) {
				return apperr.Invalid("preferences", "delivery must be immediate, digest or off, got %q", d)
			}
			if k.Portal && d == Digest {
				return apperr.Invalid("preferences", "%s are emailed immediately or not at all", k.Label)
			}
			cur.Deliveries[kind] = d
		}
		if p.DigestHour != nil {
			if *p.DigestHour < 0 || *p.DigestHour > 23 {
				return apperr.Invalid("digestHour", "the digest hour must be between 0 and 23")
			}
			cur.DigestHour = *p.DigestHour
		}
		if p.TimeZone != nil {
			tz := strings.TrimSpace(*p.TimeZone)
			if _, err := LoadZone(tz); err != nil || tz == "" {
				return apperr.Invalid("timeZone", "unknown time zone %q (use a name such as Europe/London)", tz)
			}
			cur.TimeZone = tz
		}
		if p.Paused != nil {
			cur.Paused = *p.Paused
		}
		raw, err := json.Marshal(cur.Deliveries)
		if err != nil {
			return err
		}
		row, err := q.UpsertEmailSettings(ctx, dbq.UpsertEmailSettingsParams{
			UserID: userID, Deliveries: raw, DigestHour: int16(cur.DigestHour), TimeZone: cur.TimeZone, Paused: cur.Paused,
		})
		if err != nil {
			return err
		}
		out = FromRow(row)
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Sending

// recipient is the person an email goes to, with their settings.
type recipient struct {
	user     dbq.User
	settings Settings
	loc      *time.Location
	// ok: they can receive email now (active person, not paused).
	ok bool
}

func (s *Service) recipient(ctx context.Context, q *dbq.Queries, id uuid.UUID) (recipient, error) {
	u, err := q.GetUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return recipient{}, nil
	}
	if err != nil {
		return recipient{}, err
	}
	st, err := getSettings(ctx, q, id)
	if err != nil {
		return recipient{}, err
	}
	ok := u.Active && u.DeletedAt == nil && u.Kind == "human" && u.Email != "" && !st.Paused
	return recipient{user: u, settings: st, loc: st.Location(), ok: ok}, nil
}

type pending = dbq.PendingEmailNotificationsRow

// issueGroup is what happened to one issue that the recipient may still see.
type issueGroup struct {
	issue    dbq.EmailIssuesRow
	key      string
	events   []pending
	watching bool
}

// group sorts notifications by issue (in order of first appearance), drops kinds the person
// has since switched off and issues they can no longer browse.
func (s *Service) group(ctx context.Context, q *dbq.Queries, r recipient, rows []pending) ([]*issueGroup, error) {
	var order []uuid.UUID
	byIssue := map[uuid.UUID]*issueGroup{}
	for _, n := range rows {
		if n.IssueID == nil || r.settings.Delivery(n.Kind) == Off {
			continue
		}
		g, ok := byIssue[*n.IssueID]
		if !ok {
			g = &issueGroup{}
			byIssue[*n.IssueID] = g
			order = append(order, *n.IssueID)
		}
		g.events = append(g.events, n)
	}
	if len(order) == 0 {
		return nil, nil
	}
	issues, err := q.EmailIssues(ctx, order)
	if err != nil {
		return nil, err
	}
	watched, err := q.WatchedAmong(ctx, dbq.WatchedAmongParams{UserID: r.user.ID, IssueIds: order})
	if err != nil {
		return nil, err
	}
	browsable := map[uuid.UUID]bool{}
	for _, is := range issues {
		g := byIssue[is.ID]
		g.issue, g.key = is, fmt.Sprintf("%s-%d", is.ProjectKey, is.Number)
		g.watching = slices.Contains(watched, is.ID)
		if _, seen := browsable[is.ProjectID]; !seen {
			ok := false
			if p, err := q.GetProject(ctx, is.ProjectID); err == nil {
				if ok, err = canBrowse(ctx, q, r.user, p); err != nil {
					return nil, err
				}
			}
			browsable[is.ProjectID] = ok
		}
	}
	var out []*issueGroup
	for _, id := range order {
		g := byIssue[id]
		if g.key != "" && browsable[g.issue.ProjectID] {
			out = append(out, g)
		}
	}
	return out, nil
}

// canBrowse reports whether a person may still see a project: the rule of
// tracker.Service.userCanBrowse, read through q so a sender holding locked rows never needs a
// second pool connection (many concurrent email jobs could otherwise exhaust the pool).
func canBrowse(ctx context.Context, q *dbq.Queries, u dbq.User, p dbq.Project) (bool, error) {
	if !u.Active || u.DeletedAt != nil {
		return false, nil
	}
	role := u.OrgRole
	roles, err := q.EffectiveOrgRoles(ctx, u.ID)
	if err != nil {
		return false, err
	}
	for _, r := range roles {
		role = authz.HigherOrgRole(role, r)
	}
	access, err := (&authz.Resolver{Q: q}).Access(ctx, authz.UserActor(u, role), p)
	return access.CanBrowse(), err
}

func ids(rows []pending) []int64 {
	out := make([]int64, len(rows))
	for i, n := range rows {
		out[i] = n.ID
	}
	return out
}

// SendImmediate emails a person everything pending for them on one issue (jobs.EmailNotify).
func (s *Service) SendImmediate(ctx context.Context, j jobs.EmailNotify) error {
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		rows, err := q.PendingEmailNotifications(ctx, dbq.PendingEmailNotificationsParams{UserID: j.UserID, Delivery: Immediate, IssueID: &j.IssueID})
		if err != nil || len(rows) == 0 {
			return err
		}
		r, err := s.recipient(ctx, q, j.UserID)
		if err != nil {
			return err
		}
		if r.ok {
			groups, err := s.group(ctx, q, r, rows)
			if err != nil {
				return err
			}
			if len(groups) > 0 {
				msg, err := s.immediateEmail(r, groups[0])
				if err != nil {
					return err
				}
				if err := mail.Queue(ctx, s.Jobs, tx, msg); err != nil {
					return err
				}
			}
		}
		return q.MarkNotificationsEmailed(ctx, ids(rows))
	})
}

// QueueDigests queues a digest for everyone due one (jobs.EmailDigests, periodic). It also
// requeues immediate emails whose job should long have run, as a safety net.
func (s *Service) QueueDigests(ctx context.Context) error {
	now := s.Now()
	stale, err := s.Q.StaleImmediateEmails(ctx, now.Add(-s.Cfg.EmailBatchWindow-5*time.Minute))
	if err != nil {
		return err
	}
	for _, st := range stale {
		if err := Schedule(ctx, s.Jobs, s.Cfg.EmailBatchWindow, now, st.UserID, st.IssueID); err != nil {
			return err
		}
	}
	cands, err := s.Q.DigestCandidates(ctx)
	if err != nil {
		return err
	}
	queued := 0
	for _, c := range cands {
		st := Defaults()
		if c.DigestHour != nil {
			st.DigestHour = int(*c.DigestHour)
		}
		if c.TimeZone != nil {
			st.TimeZone = *c.TimeZone
		}
		if !st.DigestDue(c.Oldest, now) {
			continue
		}
		if _, err := s.Jobs.Insert(ctx, jobs.EmailDigest{UserID: c.UserID}, nil); err != nil {
			return err
		}
		queued++
	}
	if queued > 0 || len(stale) > 0 {
		s.Log.Info("email digests queued", "digests", queued, "requeued_immediate", len(stale))
	}
	return nil
}

// SendDigest emails a person their digest: everything waiting since the last one, by issue
// (jobs.EmailDigest).
func (s *Service) SendDigest(ctx context.Context, j jobs.EmailDigest) error {
	now := s.Now()
	return db.Tx(ctx, s.Pool, func(tx pgx.Tx, q *dbq.Queries) error {
		rows, err := q.PendingEmailNotifications(ctx, dbq.PendingEmailNotificationsParams{UserID: j.UserID, Delivery: Digest})
		if err != nil || len(rows) == 0 {
			return err
		}
		r, err := s.recipient(ctx, q, j.UserID)
		if err != nil {
			return err
		}
		if r.ok {
			groups, err := s.group(ctx, q, r, rows)
			if err != nil {
				return err
			}
			if len(groups) > 0 {
				msg, err := s.digestEmail(r, groups)
				if err != nil {
					return err
				}
				if err := mail.Queue(ctx, s.Jobs, tx, msg); err != nil {
					return err
				}
			}
		}
		if err := q.MarkNotificationsEmailed(ctx, ids(rows)); err != nil {
			return err
		}
		return q.RecordDigestSent(ctx, dbq.RecordDigestSentParams{UserID: j.UserID, LastDigestAt: &now})
	})
}

// SendTest emails a test message to a person right away (not queued), so a broken SMTP
// configuration shows up at once.
func (s *Service) SendTest(ctx context.Context, to dbq.User, by string) (mail.Message, error) {
	where := "kept in memory (no SMTP server is configured)"
	if s.Cfg.SMTP.Host != "" {
		where = fmt.Sprintf("sent through %s:%d", s.Cfg.SMTP.Host, s.Cfg.SMTP.Port)
	}
	e := Email{
		Brand:     s.Brand(),
		Subject:   "Test email from " + s.Brand(),
		Preheader: "Outgoing email works.",
		Headline:  "Outgoing email works",
		Intro: []string{
			fmt.Sprintf("%s sent this test from Administration → Email at %s UTC. It was %s.", by, s.Now().UTC().Format("2 Jan 2006 15:04"), where),
			"Notification emails link to " + s.Cfg.PublicURL + ".",
		},
		Button:    &Link{Label: "Open " + s.Brand(), URL: s.Cfg.PublicURL + "/"},
		Reason:    "You're receiving this because you asked for a test email.",
		ManageURL: s.ManageURL(),
	}
	msg, err := s.message(to.Email, e, "", "")
	if err != nil {
		return msg, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return msg, s.Mailer.Send(ctx, msg)
}

// message renders an email and adds the headers: automatic (no auto-replies), one-click
// unsubscribe and, for issue emails, a thread reference so clients group them by issue.
func (s *Service) message(to string, e Email, unsubURL, thread string) (mail.Message, error) {
	text, html, err := e.Render()
	if err != nil {
		return mail.Message{}, err
	}
	h := map[string]string{"Auto-Submitted": "auto-generated", "X-Auto-Response-Suppress": "All"}
	if unsubURL != "" {
		h["List-Unsubscribe"] = "<" + unsubURL + ">"
		h["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
	}
	if thread != "" {
		h["References"] = thread
		h["In-Reply-To"] = thread
	}
	return mail.Message{To: []string{to}, Subject: e.Subject, Text: text, HTML: html, Headers: h}, nil
}

func (s *Service) threadID(issueID uuid.UUID) string {
	host := "wfm"
	if u, err := url.Parse(s.Cfg.PublicURL); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	return "<issue." + issueID.String() + "@" + host + ">"
}
