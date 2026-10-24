// Package chat formats notifications for Slack and Mattermost incoming webhooks. It is pure:
// callers describe what happened in an Event and get back the JSON body to post.
package chat

import (
	"fmt"
	"net/url"
	"strings"
)

// Providers this package formats for. Mattermost accepts Slack-style payloads but renders
// Markdown and attachments rather than Block Kit, so each gets its own shape.
const (
	Slack      = "slack"
	Mattermost = "mattermost"
)

// Events channels can subscribe to ("*" means all of them).
var Events = []string{
	"issue.created", "issue.transitioned", "issue.assigned", "comment.created",
	"sprint.started", "sprint.completed", "release.released",
	"pr.opened", "pr.merged", "pipeline.failed",
}

// Event is something worth telling a channel about. Unused fields stay empty.
type Event struct {
	Kind        string // one of Events, or "automation" / "test"
	ProjectKey  string
	ProjectName string
	ProjectURL  string
	Actor       string // display name of who did it ("" for automation or the system)

	IssueKey      string
	IssueURL      string
	Summary       string
	Type          string // issue type name
	Status        string
	Category      string // status category of the issue (or the target status)
	Priority      string
	Assignee      string
	FromStatus    string
	ToStatus      string
	CommentBody   string
	Name          string // sprint or release name
	Goal          string // sprint goal
	MovedIssues   int    // issues carried over when a sprint completed
	PRNumber      string
	PRTitle       string
	PRURL         string
	Branch        string
	PipelineName  string
	PipelineURL   string
	Text          string // free text (automation messages and tests)
	ExtraContext  string
	RepositoryRef string
}

// Message is the provider-neutral shape of a notification.
type Message struct {
	Text    string // one line, Markdown-ish: *bold*, links via Link()
	Title   string
	URL     string
	Body    string // optional quoted text (comments)
	Fields  [][2]string
	Color   string
	Context string
}

var categoryColors = map[string]string{"todo": "#8b93a1", "in_progress": "#2563eb", "done": "#16a34a"}

// Format turns an Event into a Message.
func Format(e Event) Message {
	who := e.Actor
	if who == "" {
		who = "Automation"
	}
	issue := func() string { return link(e.IssueURL, e.IssueKey) }
	m := Message{Title: e.Summary, URL: e.IssueURL, Color: categoryColors[e.Category], Context: e.ProjectName}
	if e.IssueKey != "" {
		m.Title = e.IssueKey + " " + e.Summary
	}
	switch e.Kind {
	case "issue.created":
		m.Text = fmt.Sprintf("%s created %s %s", bold(who), article(e.Type), issue())
		m.Fields = nonEmpty([2]string{"Status", e.Status}, [2]string{"Priority", title(e.Priority)}, [2]string{"Assignee", or(e.Assignee, "Unassigned")})
	case "issue.transitioned":
		m.Text = fmt.Sprintf("%s moved %s from %s to %s", bold(who), issue(), bold(e.FromStatus), bold(e.ToStatus))
	case "issue.assigned":
		if e.Assignee == "" {
			m.Text = fmt.Sprintf("%s unassigned %s", bold(who), issue())
		} else {
			m.Text = fmt.Sprintf("%s assigned %s to %s", bold(who), issue(), bold(e.Assignee))
		}
	case "comment.created":
		m.Text = fmt.Sprintf("%s commented on %s", bold(who), issue())
		m.Body = excerpt(e.CommentBody, 400)
	case "sprint.started":
		m.Title, m.URL, m.Color = e.Name, e.ProjectURL, categoryColors["in_progress"]
		m.Text = fmt.Sprintf("%s started %s", bold(who), bold(e.Name))
		m.Body = e.Goal
	case "sprint.completed":
		m.Title, m.URL, m.Color = e.Name, e.ProjectURL, categoryColors["done"]
		m.Text = fmt.Sprintf("%s completed %s", bold(who), bold(e.Name))
		if e.MovedIssues > 0 {
			m.Text += fmt.Sprintf(" — %d open issue%s carried over", e.MovedIssues, plural(e.MovedIssues))
		}
	case "release.released":
		m.Title, m.URL, m.Color = e.Name, e.ProjectURL, categoryColors["done"]
		m.Text = fmt.Sprintf("%s released %s", bold(who), bold(e.Name))
	case "pr.opened", "pr.merged":
		verb := "opened"
		if e.Kind == "pr.merged" {
			verb, m.Color = "merged", categoryColors["done"]
		}
		m.Text = fmt.Sprintf("Pull request %s %s for %s", link(e.PRURL, "#"+e.PRNumber+" "+e.PRTitle), verb, issue())
		m.Fields = nonEmpty([2]string{"Branch", e.Branch}, [2]string{"Repository", e.RepositoryRef})
	case "pipeline.failed":
		m.Color = "#dc2626"
		m.Text = fmt.Sprintf("Pipeline %s failed for %s", link(e.PipelineURL, or(e.PipelineName, "pipeline")), issue())
		m.Fields = nonEmpty([2]string{"Branch", e.Branch}, [2]string{"Repository", e.RepositoryRef})
	default: // automation, test
		m.Text = e.Text
	}
	if e.ExtraContext != "" {
		m.Context = strings.TrimSpace(m.Context + " · " + e.ExtraContext)
	}
	return m
}

// Payload renders a Message for a provider's incoming webhook.
func Payload(provider string, m Message) map[string]any {
	if provider == Mattermost {
		return mattermostPayload(m)
	}
	return slackPayload(m)
}

func slackPayload(m Message) map[string]any {
	text := slackText(m.Text)
	blocks := []any{map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text}}}
	var att []string
	if m.Title != "" {
		t := slackEscape(m.Title)
		if m.URL != "" {
			t = "<" + m.URL + "|" + t + ">"
		}
		att = append(att, "*"+t+"*")
	}
	if m.Body != "" {
		att = append(att, "> "+strings.ReplaceAll(slackEscape(m.Body), "\n", "\n> "))
	}
	if len(att) > 0 {
		blocks = append(blocks, map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": strings.Join(att, "\n")}})
	}
	if len(m.Fields) > 0 {
		var fs []any
		for _, f := range m.Fields {
			fs = append(fs, map[string]any{"type": "mrkdwn", "text": "*" + slackEscape(f[0]) + "*\n" + slackEscape(f[1])})
		}
		blocks = append(blocks, map[string]any{"type": "section", "fields": fs})
	}
	if m.Context != "" {
		blocks = append(blocks, map[string]any{"type": "context", "elements": []any{map[string]any{"type": "mrkdwn", "text": slackEscape(m.Context)}}})
	}
	// "text" is the notification and screen-reader fallback.
	return map[string]any{"text": plain(m.Text), "blocks": blocks, "unfurl_links": false}
}

func mattermostPayload(m Message) map[string]any {
	att := map[string]any{"fallback": plain(m.Text), "color": or(m.Color, "#8b93a1")}
	if m.Title != "" {
		att["title"] = m.Title
		if m.URL != "" {
			att["title_link"] = m.URL
		}
	}
	if m.Body != "" {
		att["text"] = "> " + strings.ReplaceAll(m.Body, "\n", "\n> ")
	}
	if len(m.Fields) > 0 {
		var fs []any
		for _, f := range m.Fields {
			fs = append(fs, map[string]any{"title": f[0], "value": f[1], "short": true})
		}
		att["fields"] = fs
	}
	if m.Context != "" {
		att["footer"] = m.Context
	}
	return map[string]any{"text": markdownText(m.Text), "attachments": []any{att}}
}

// Inline markup in Message.Text: bold(x) → "**x**", link(u, t) → "[t](u)". Slack uses its own
// mrkdwn dialect, so the text is translated when rendering.

func bold(s string) string { return "**" + escapeMarkup(s) + "**" }

func link(u, text string) string {
	if u == "" {
		return escapeMarkup(text)
	}
	return "[" + escapeMarkup(text) + "](" + u + ")"
}

func escapeMarkup(s string) string {
	return strings.NewReplacer("[", "(", "]", ")", "**", "*").Replace(s)
}

// markdownText is Message.Text for Mattermost (Markdown already).
func markdownText(s string) string { return s }

// slackText converts **bold** and [text](url) to Slack mrkdwn, escaping &, < and >.
func slackText(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		switch {
		case strings.HasPrefix(s, "**"):
			end := strings.Index(s[2:], "**")
			if end < 0 {
				b.WriteString(slackEscape(s))
				return b.String()
			}
			b.WriteString("*" + slackEscape(s[2:2+end]) + "*")
			s = s[4+end:]
		case s[0] == '[':
			mid := strings.Index(s, "](")
			end := -1
			if mid > 0 {
				end = strings.IndexByte(s[mid+2:], ')')
			}
			if end < 0 {
				b.WriteString(slackEscape(s[:1]))
				s = s[1:]
				continue
			}
			b.WriteString("<" + s[mid+2:mid+2+end] + "|" + slackEscape(s[1:mid]) + ">")
			s = s[mid+3+end:]
		default:
			next := strings.IndexAny(s[1:], "*[")
			if next < 0 {
				b.WriteString(slackEscape(s))
				return b.String()
			}
			b.WriteString(slackEscape(s[:next+1]))
			s = s[next+1:]
		}
	}
	return b.String()
}

// plain strips the inline markup for notification fallbacks.
func plain(s string) string {
	out := slackText(s)
	out = strings.NewReplacer("*", "").Replace(out)
	var b strings.Builder
	for len(out) > 0 {
		i := strings.IndexByte(out, '<')
		if i < 0 {
			b.WriteString(out)
			break
		}
		b.WriteString(out[:i])
		j := strings.IndexByte(out[i:], '>')
		if j < 0 {
			b.WriteString(out[i:])
			break
		}
		inner := out[i+1 : i+j]
		if k := strings.IndexByte(inner, '|'); k >= 0 {
			inner = inner[k+1:]
		}
		b.WriteString(inner)
		out = out[i+j+1:]
	}
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(b.String())
}

func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func excerpt(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func nonEmpty(fields ...[2]string) [][2]string {
	var out [][2]string
	for _, f := range fields {
		if f[1] != "" {
			out = append(out, f)
		}
	}
	return out
}

func article(typ string) string {
	if typ == "" {
		return "an issue"
	}
	if strings.ContainsRune("AEIOUaeiou", rune(typ[0])) {
		return "an " + strings.ToLower(typ)
	}
	return "a " + strings.ToLower(typ)
}

func title(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ValidateWebhookURL checks an incoming-webhook URL for a provider. Slack's are always HTTPS;
// self-hosted Mattermost may use plain HTTP inside a network.
func ValidateWebhookURL(provider, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("enter the channel's incoming webhook URL")
	}
	if provider == Slack && u.Scheme != "https" {
		return fmt.Errorf("Slack webhook URLs use https")
	}
	return nil
}

// URLHint identifies a webhook without revealing it: host, then the last four characters.
func URLHint(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "…"
	}
	tail := strings.TrimRight(u.Path, "/")
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return u.Host + "/…" + tail
}
