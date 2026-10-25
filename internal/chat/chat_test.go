package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSlackText(t *testing.T) {
	cases := map[string]string{
		"**Ada** moved [PLAT-1](https://x/browse/PLAT-1) from **To Do** to **Done**": "*Ada* moved <https://x/browse/PLAT-1|PLAT-1> from *To Do* to *Done*",
		"a < b & c > d":       "a &lt; b &amp; c &gt; d",
		"[broken link":        "[broken link",
		"price [x](y) * star": "price <y|x> * star",
	}
	for in, want := range cases {
		if got := slackText(in); got != want {
			t.Errorf("slackText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := plain("**Ada** moved [PLAT-1](https://x) to <done> & more"); got != "Ada moved PLAT-1 to <done> & more" {
		t.Errorf("plain = %q", got)
	}
}

func TestFormat(t *testing.T) {
	e := Event{Kind: "issue.transitioned", Actor: "Ada [admin]", IssueKey: "PLAT-1", IssueURL: "https://wfm/browse/PLAT-1",
		Summary: "Fix <login>", FromStatus: "In Review", ToStatus: "Done", Category: "done", ProjectName: "Platform"}
	m := Format(e)
	if m.Text != "**Ada (admin)** moved [PLAT-1](https://wfm/browse/PLAT-1) from **In Review** to **Done**" {
		t.Fatalf("text = %q", m.Text)
	}
	slack := encode(Payload(Slack, m))
	for _, want := range []string{`"text":"Ada (admin) moved PLAT-1 from In Review to Done"`, `<https://wfm/browse/PLAT-1|PLAT-1 Fix &lt;login&gt;>`, `"type":"context"`} {
		if !strings.Contains(slack, want) {
			t.Errorf("slack payload lacks %s:\n%s", want, slack)
		}
	}
	mm := encode(Payload(Mattermost, m))
	for _, want := range []string{`"title_link":"https://wfm/browse/PLAT-1"`, `"color":"#16a34a"`, `"footer":"Platform"`} {
		if !strings.Contains(mm, want) {
			t.Errorf("mattermost payload lacks %s:\n%s", want, mm)
		}
	}

	c := Format(Event{Kind: "comment.created", Actor: "Bob", IssueKey: "PLAT-2", CommentBody: strings.Repeat("x", 500)})
	if !strings.HasSuffix(c.Body, "…") || len([]rune(c.Body)) != 401 {
		t.Errorf("comment excerpt = %d runes", len([]rune(c.Body)))
	}
	if got := Format(Event{Kind: "issue.created", Type: "Epic", IssueKey: "A-1"}).Text; !strings.Contains(got, "an epic") {
		t.Errorf("article: %q", got)
	}
	if got := Format(Event{Kind: "sprint.completed", Actor: "Ada", Name: "Sprint 3", MovedIssues: 2}).Text; !strings.Contains(got, "2 open issues carried over") {
		t.Errorf("sprint: %q", got)
	}
}

func encode(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return b.String()
}

func TestWebhookURL(t *testing.T) {
	if ValidateWebhookURL(Slack, "http://hooks.slack.com/services/x") == nil {
		t.Error("slack must use https")
	}
	if ValidateWebhookURL(Mattermost, "http://mattermost.internal/hooks/abc") != nil {
		t.Error("mattermost may use http")
	}
	if ValidateWebhookURL(Slack, "not a url") == nil {
		t.Error("garbage accepted")
	}
	if got := URLHint("https://hooks.slack.com/services/T000/B000/abcdWXYZ"); got != "hooks.slack.com/…WXYZ" {
		t.Errorf("hint = %q", got)
	}
}
