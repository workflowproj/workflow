package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/authz"
	"github.com/workflowproj/workflow/internal/chat"
	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/secure"
)

// HandleChatCommand answers the /wfm slash command from Slack (POST /hooks/chat/slack) and
// Mattermost (POST /hooks/chat/mattermost):
//
//	/wfm PLAT-12            show an issue
//	/wfm search <JQL>       up to five matching issues
//	/wfm help
//
// Commands are read-only and only see open projects (or WFM_CHAT_COMMAND_PROJECTS), because chat
// workspaces may include people without accounts here. Answers are visible only to the caller.
func (s *Service) HandleChatCommand(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	switch provider {
	case chat.Slack:
		if s.Cfg.SlackSigningSecret == "" {
			http.NotFound(w, r)
			return
		}
		if !verifySlack(r, body, s.Cfg.SlackSigningSecret, time.Now()) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	case chat.Mattermost:
		if len(s.Cfg.MattermostCommandTokens) == 0 {
			http.NotFound(w, r)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if provider == chat.Mattermost && !slices.ContainsFunc(s.Cfg.MattermostCommandTokens, func(t string) bool { return secure.Equal(t, form.Get("token")) }) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	m := s.chatCommand(r, strings.TrimSpace(form.Get("text")), form.Get("command"))
	out := chat.Payload(provider, m)
	out["response_type"] = "ephemeral"
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// verifySlack checks Slack's v0 request signature and rejects replays older than five minutes.
func verifySlack(r *http.Request, body []byte, secret string, now time.Time) bool {
	ts := r.Header.Get("X-Slack-Request-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || now.Sub(time.Unix(sec, 0)).Abs() > 5*time.Minute {
		return false
	}
	want := "v0=" + secure.HMACSHA256Hex(secret, []byte("v0:"+ts+":"+string(body)))
	return secure.Equal(r.Header.Get("X-Slack-Signature"), want)
}

func (s *Service) chatCommand(r *http.Request, text, command string) chat.Message {
	if command == "" {
		command = "/wfm"
	}
	ctx := r.Context()
	help := chat.Message{Text: fmt.Sprintf("**%s KEY-123** shows an issue · **%s search** *query* lists matches (up to five), e.g. `%s search assignee = \"ada@example.com\" AND statusCategory != Done`", command, command, command)}
	if text == "" || strings.EqualFold(text, "help") {
		return help
	}
	projects, err := s.commandProjects(ctx)
	if err != nil {
		return chat.Message{Text: "Something went wrong; try again later."}
	}
	if q, ok := strings.CutPrefix(text, "search "); ok || strings.HasPrefix(strings.ToLower(text), "search ") {
		if !ok {
			q = text[len("search "):]
		}
		return s.commandSearch(r, projects, strings.TrimSpace(q))
	}
	issue, err := s.Tracker.ResolveIssue(ctx, s.Q, strings.ToUpper(strings.Fields(text)[0]))
	p, visible := projects[issueProjectID(issue, err)]
	if err != nil || !visible {
		return chat.Message{Text: fmt.Sprintf("No issue %s that chat can show. %s", strings.Fields(text)[0], help.Text)}
	}
	view, err := s.Tracker.IssueView(ctx, issue)
	if err != nil {
		return chat.Message{Text: "Something went wrong; try again later."}
	}
	assignee := "Unassigned"
	if view.Assignee != nil {
		assignee = view.Assignee.DisplayName
	}
	return chat.Message{
		Text:    fmt.Sprintf("[%s](%s) %s", view.Key, s.issueURL(view.Key), strings.NewReplacer("[", "(", "]", ")").Replace(view.Summary)),
		Fields:  [][2]string{{"Status", view.Status.Name}, {"Assignee", assignee}, {"Type", view.Type.Name}, {"Priority", strings.ToUpper(string(view.Priority)[:1]) + string(view.Priority)[1:]}},
		Color:   map[string]string{"todo": "#8b93a1", "in_progress": "#2563eb", "done": "#16a34a"}[string(view.Status.Category)],
		Context: p.Name,
	}
}

func issueProjectID(i dbq.Issue, err error) uuid.UUID {
	if err != nil {
		return uuid.Nil
	}
	return i.ProjectID
}

func (s *Service) commandSearch(r *http.Request, projects map[uuid.UUID]dbq.Project, jql string) chat.Message {
	ids := make([]uuid.UUID, 0, len(projects))
	for id := range projects {
		ids = append(ids, id)
	}
	issues, total, err := s.Tracker.SearchIn(r.Context(), authz.SystemActor(), ids, jql, 5)
	if err != nil {
		return chat.Message{Text: "That query did not work: " + err.Error()}
	}
	if total == 0 {
		return chat.Message{Text: "No issues match."}
	}
	views, err := s.Tracker.IssueViews(r.Context(), issues)
	if err != nil {
		return chat.Message{Text: "Something went wrong; try again later."}
	}
	lines := []string{fmt.Sprintf("**%d** issue%s match", total, map[bool]string{true: "", false: "s"}[total == 1])}
	for _, v := range views {
		lines = append(lines, fmt.Sprintf("• [%s](%s) %s — %s", v.Key, s.issueURL(v.Key), strings.NewReplacer("[", "(", "]", ")").Replace(v.Summary), v.Status.Name))
	}
	if total > len(views) {
		lines = append(lines, fmt.Sprintf("[See all %d in Workflow](%s/issues?jql=%s)", total, s.Cfg.PublicURL, url.QueryEscape(jql)))
	}
	return chat.Message{Text: strings.Join(lines, "\n")}
}

// commandProjects are the projects slash commands may read.
func (s *Service) commandProjects(ctx context.Context) (map[uuid.UUID]dbq.Project, error) {
	all, err := s.Q.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]dbq.Project{}
	for _, p := range all {
		listed := slices.ContainsFunc(s.Cfg.ChatCommandProjects, func(k string) bool { return strings.EqualFold(strings.TrimSpace(k), p.Key) })
		if listed || (len(s.Cfg.ChatCommandProjects) == 0 && p.OpenRoleID != nil) {
			out[p.ID] = p
		}
	}
	return out, nil
}
