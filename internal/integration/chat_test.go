package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// chatReceiver records incoming-webhook posts the way Slack or Mattermost would receive them.
type chatReceiver struct {
	*httptest.Server
	mu     sync.Mutex
	posts  []obj
	status int
}

func newChatReceiver(t *testing.T, tls bool) *chatReceiver {
	r := &chatReceiver{status: 200}
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		var body obj
		_ = json.Unmarshal(raw, &body)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.posts = append(r.posts, body)
		w.WriteHeader(r.status)
		_, _ = w.Write([]byte("ok"))
	})
	if tls {
		r.Server = httptest.NewTLSServer(h)
	} else {
		r.Server = httptest.NewServer(h)
	}
	t.Cleanup(r.Close)
	return r
}

func (r *chatReceiver) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, p := range r.posts {
		out = append(out, str(p, "text"))
	}
	return out
}

func (r *chatReceiver) reset() {
	r.mu.Lock()
	r.posts = nil
	r.mu.Unlock()
}

func TestChatChannels(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	mel := e.login("mel@example.com", "Mel")
	admin.createProject("CHAT", "scrum")
	slack := newChatReceiver(t, true)
	mm := newChatReceiver(t, false)
	e.App.Automation.HTTP = slack.Client() // trusts the TLS test server; plain HTTP works too

	mel.must("POST", "/api/v1/projects/CHAT/chat-channels", obj{"provider": "slack", "name": "#x", "webhookUrl": slack.URL, "events": []string{"*"}}, 403, nil)
	for _, bad := range []obj{
		{"provider": "slack", "name": "#x", "webhookUrl": strings.Replace(slack.URL, "https", "http", 1), "events": []string{"*"}},
		{"provider": "slack", "name": "#x", "webhookUrl": slack.URL, "events": []string{"issue.exploded"}},
		{"provider": "slack", "name": "#x", "webhookUrl": slack.URL, "events": []string{"*"}, "jql": "type =="},
		{"provider": "teams", "name": "#x", "webhookUrl": slack.URL, "events": []string{"*"}},
	} {
		admin.must("POST", "/api/v1/projects/CHAT/chat-channels", bad, 400, nil)
	}
	var sc, mc obj
	admin.must("POST", "/api/v1/projects/CHAT/chat-channels", obj{"provider": "slack", "name": "#bugs", "webhookUrl": slack.URL + "/services/T1/B2/secretXYZ1",
		"events": []string{"issue.created", "issue.transitioned", "comment.created"}, "jql": "type = Bug"}, 201, &sc)
	admin.must("POST", "/api/v1/projects/CHAT/chat-channels", obj{"provider": "mattermost", "name": "~team", "webhookUrl": mm.URL + "/hooks/abc", "events": []string{"*"}}, 201, &mc)
	if !strings.HasSuffix(str(sc, "urlHint"), "…XYZ1") || strings.Contains(string(admin.must("GET", "/api/v1/projects/CHAT/chat-channels", nil, 200, nil).Body), "secretXYZ1") {
		t.Fatalf("webhook URL must never be returned: %v", sc)
	}

	bug := admin.createIssue("CHAT", "bug", "Checkout <crashes>", obj{"priority": "high"})
	admin.createIssue("CHAT", "task", "Write docs", nil)
	e.DrainJobs()
	eq(t, len(slack.texts()), 1, "slack: only the bug (JQL filter)")
	eq(t, slack.texts()[0], "Ada created a bug CHAT-1", "slack fallback text")
	eq(t, len(mm.texts()), 2, "mattermost: both issues")
	if _, ok := slack.posts[0]["blocks"]; !ok {
		t.Fatalf("slack payload uses Block Kit: %v", slack.posts[0])
	}
	att := mm.posts[0]["attachments"].([]any)[0].(obj)
	eq(t, str(att, "title"), "CHAT-1 Checkout <crashes>", "mattermost attachment title")
	if !strings.HasSuffix(str(att, "title_link"), "/browse/CHAT-1") {
		t.Fatalf("issue link: %v", att)
	}

	slack.reset()
	mm.reset()
	admin.transition(str(bug, "key"), "start-work", 200) // also assigns Ada (post-function)
	admin.must("POST", "/api/v1/issues/"+str(bug, "key")+"/comments", obj{"body": "Looking into it"}, 201, nil)
	e.DrainJobs()
	sameStrings(t, slack.texts(), []string{"Ada moved CHAT-1 from To Do to In Progress", "Ada commented on CHAT-1"}, "slack transition + comment")
	if !strings.Contains(strings.Join(mm.texts(), "\n"), "**Ada** moved [CHAT-1](") {
		t.Fatalf("mattermost markdown: %v", mm.texts())
	}

	// Sprint events go to channels subscribed to them; assignment is its own event.
	mm.reset()
	var sprints []obj
	admin.get("/api/v1/projects/CHAT/sprints", &sprints)
	admin.must("POST", "/api/v1/sprints/"+str(sprints[0], "id")+"/start", obj{"startDate": "2026-11-02", "endDate": "2026-11-13", "goal": "Ship it"}, 200, nil)
	admin.must("PATCH", "/api/v1/issues/CHAT-2", obj{"assigneeId": mel.id()}, 200, nil)
	e.DrainJobs()
	joined := strings.Join(mm.texts(), "\n")
	for _, want := range []string{"**Ada** started **CHAT Sprint 1**", "**Ada** assigned [CHAT-2]("} {
		if !strings.Contains(joined, want) {
			t.Fatalf("mattermost lacks %q:\n%s", want, joined)
		}
	}

	// Automation can post too.
	admin.must("POST", "/api/v1/projects/CHAT/automation-rules", obj{"name": "x", "trigger": obj{"type": "issue.created"},
		"actions": []obj{{"type": "chat", "channelId": "00000000-0000-0000-0000-000000000000", "body": "x"}}}, 400, nil)
	admin.must("POST", "/api/v1/projects/CHAT/automation-rules", obj{"name": "Celebrate", "trigger": obj{"type": "issue.transitioned", "toCategory": "done"},
		"actions": []obj{{"type": "chat", "channelId": mc["id"], "body": "{{issue.key}} is done, thanks {{event.to}}!"}}}, 201, nil)
	mm.reset()
	admin.transition(str(bug, "key"), "done", 200)
	e.DrainJobs()
	if !strings.Contains(strings.Join(mm.texts(), "\n"), "CHAT-1 is done, thanks Done!") {
		t.Fatalf("automation chat message missing: %v", mm.texts())
	}

	// Test messages, delivery log, and a receiver that rejects the post.
	slack.reset()
	admin.must("POST", "/api/v1/chat-channels/"+str(sc, "id")+"/test", nil, 202, nil)
	e.DrainJobs()
	eq(t, len(slack.texts()), 1, "test message")
	mm.mu.Lock()
	mm.status = 404
	mm.mu.Unlock()
	admin.must("POST", "/api/v1/chat-channels/"+str(mc, "id")+"/test", nil, 202, nil)
	e.DrainJobs()
	var dels []obj
	admin.get("/api/v1/chat-channels/"+str(mc, "id")+"/deliveries", &dels)
	eq(t, str(dels[0], "status"), "failed", "rejected delivery recorded")
	eq(t, str(dels[1], "status"), "delivered", "earlier delivery")

	// Paused channels stay quiet; updating keeps the URL unless a new one is given.
	admin.must("PATCH", "/api/v1/chat-channels/"+str(sc, "id"), obj{"active": false}, 200, nil)
	slack.reset()
	admin.createIssue("CHAT", "bug", "Another", nil)
	e.DrainJobs()
	eq(t, len(slack.texts()), 0, "paused channel")
	admin.must("DELETE", "/api/v1/chat-channels/"+str(sc, "id"), nil, 204, nil)
}

func TestChatSlashCommands(t *testing.T) {
	e := newEnv(t, withEnv("WFM_SLACK_SIGNING_SECRET", "slack-signing"), withEnv("WFM_MATTERMOST_COMMAND_TOKENS", "mm-token"))
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("APP", "kanban")
	admin.must("POST", "/api/v1/projects", obj{"key": "SEC", "name": "Secret", "template": "kanban", "access": "private"}, 201, nil)
	admin.createIssue("APP", "bug", "Login fails on Safari", obj{"priority": "high"})
	admin.createIssue("APP", "task", "Upgrade Postgres", nil)
	admin.createIssue("SEC", "task", "Acquisition plan", nil)

	slackCmd := func(text string, ts int64, secret string) Response {
		body := url.Values{"command": {"/wfm"}, "text": {text}, "user_name": {"ada"}}.Encode()
		stamp := strconv.FormatInt(ts, 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte("v0:" + stamp + ":" + body))
		c := e.anon()
		c.csrf = false
		return c.do("POST", "/hooks/chat/slack", body, "Content-Type", "application/x-www-form-urlencoded",
			"X-Slack-Request-Timestamp", stamp, "X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	}
	now := time.Now().Unix()
	r := slackCmd("app-1", now, "slack-signing")
	eq(t, r.Status, 200, "slack lookup")
	eq(t, str(r.JSON(), "response_type"), "ephemeral", "only the caller sees it")
	if !strings.Contains(str(r.JSON(), "text"), "APP-1 Login fails on Safari") {
		t.Fatalf("lookup answer: %s", r.Body)
	}
	eq(t, slackCmd("APP-1", now, "wrong").Status, 401, "bad signature")
	eq(t, slackCmd("APP-1", now-600, "slack-signing").Status, 401, "replayed request")
	if !strings.HasPrefix(str(slackCmd("SEC-1", now, "slack-signing").JSON(), "text"), "No issue SEC-1") {
		t.Fatal("private project issue must not be shown in chat")
	}
	if !strings.Contains(str(slackCmd(`search summary ~ "Postgres"`, now, "slack-signing").JSON(), "text"), "1 issue match") {
		t.Fatal("search answer")
	}
	if !strings.Contains(str(slackCmd("help", now, "slack-signing").JSON(), "text"), "search") {
		t.Fatal("help")
	}

	mm := func(token, text string) Response {
		c := e.anon()
		c.csrf = false
		return c.do("POST", "/hooks/chat/mattermost", url.Values{"token": {token}, "text": {text}, "command": {"/wfm"}}.Encode(), "Content-Type", "application/x-www-form-urlencoded")
	}
	eq(t, mm("nope", "APP-1").Status, 401, "mattermost token")
	r = mm("mm-token", "search project = APP ORDER BY key")
	eq(t, r.Status, 200, "mattermost search")
	if !strings.Contains(str(r.JSON(), "text"), "**2** issues match") {
		t.Fatalf("mattermost answer: %s", r.Body)
	}

	off := newEnv(t)
	c := off.anon()
	c.csrf = false
	eq(t, c.do("POST", "/hooks/chat/slack", "text=x", "Content-Type", "application/x-www-form-urlencoded").Status, 404, "disabled without a signing secret")
}
