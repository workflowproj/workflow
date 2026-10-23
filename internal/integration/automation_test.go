package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// receiver collects outbound webhook deliveries.
type receiver struct {
	*httptest.Server
	mu   sync.Mutex
	got  []delivery
	fail bool
}

type delivery struct {
	Event, Signature string
	Body             []byte
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		defer r.mu.Unlock()
		r.got = append(r.got, delivery{Event: req.Header.Get("X-WFM-Event"), Signature: req.Header.Get("X-WFM-Signature-256"), Body: body})
		if r.fail {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, d := range r.got {
		out = append(out, d.Event)
	}
	return out
}

func runs(c *Client, ruleID string) []obj {
	var out []obj
	c.get("/api/v1/automation-rules/"+ruleID+"/runs", &out)
	return out
}

func TestAutomationRules(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	mel := e.login("mel@example.com", "Mel")
	admin.createProject("AUT", "kanban")

	// Members cannot manage rules; bad rules are rejected with field errors.
	mel.must("POST", "/api/v1/projects/AUT/automation-rules", obj{"name": "x", "trigger": obj{"type": "issue.created"}, "actions": []obj{{"type": "add_label", "label": "x"}}}, 403, nil)
	bad := []obj{
		{"name": "bad cel", "trigger": obj{"type": "issue.created"}, "condition": "issue.nope ==", "actions": []obj{{"type": "add_label", "label": "x"}}},
		{"name": "bad cron", "trigger": obj{"type": "schedule", "cron": "every day", "jql": "project = AUT"}, "actions": []obj{{"type": "add_label", "label": "x"}}},
		{"name": "bad jql", "trigger": obj{"type": "schedule", "cron": "0 9 * * 1", "jql": "status =="}, "actions": []obj{{"type": "add_label", "label": "x"}}},
		{"name": "bad action", "trigger": obj{"type": "issue.created"}, "actions": []obj{{"type": "transition"}}},
		{"name": "bad status", "trigger": obj{"type": "issue.created"}, "actions": []obj{{"type": "transition", "status": "Nowhere"}}},
	}
	for _, b := range bad {
		admin.must("POST", "/api/v1/projects/AUT/automation-rules", b, 400, nil)
	}

	// Triage new bugs: condition in CEL, several actions.
	var triage obj
	admin.must("POST", "/api/v1/projects/AUT/automation-rules", obj{
		"name": "Triage bugs", "trigger": obj{"type": "issue.created"}, "condition": `issue.type == "bug"`,
		"actions": []obj{
			{"type": "set_field", "field": "priority", "value": "high"},
			{"type": "add_label", "label": "needs-triage"},
			{"type": "assign", "assignee": "reporter"},
			{"type": "comment", "body": "Thanks for reporting {{issue.key}}"},
		},
	}, 201, &triage)
	mel.createIssue("AUT", "bug", "Crash", nil)
	mel.createIssue("AUT", "task", "Not a bug", nil)
	e.DrainJobs()
	bug := admin.issue("AUT-1")
	eq(t, str(bug, "priority"), "high", "priority set")
	eq(t, fmt.Sprint(bug["labels"]), "[needs-triage]", "label added")
	eq(t, str(bug, "assignee", "email"), "mel@example.com", "assigned to reporter")
	var comments []obj
	admin.get("/api/v1/issues/AUT-1/comments", &comments)
	eq(t, str(comments[0], "body"), "Thanks for reporting AUT-1", "templated comment")
	eq(t, str(comments[0], "source"), "automation", "comment by automation")
	eq(t, str(admin.issue("AUT-2"), "priority"), "medium", "condition filtered the task")
	rs := runs(admin, str(triage, "id"))
	statuses := map[string]int{}
	for _, r := range rs {
		statuses[str(r, "status")]++
	}
	eq(t, statuses["success"], 1, "one success")
	eq(t, statuses["skipped"], 1, "one skipped by condition")

	// Mel was assigned by automation, so she gets a notification.
	var notes struct {
		Items  []obj `json:"items"`
		Unread int   `json:"unread"`
	}
	mel.get("/api/v1/notifications", &notes)
	if notes.Unread == 0 {
		t.Fatalf("expected notifications for mel: %+v", notes)
	}
	mel.must("POST", "/api/v1/notifications/read", obj{}, 204, nil)
	mel.get("/api/v1/notifications?unread=true", &notes)
	eq(t, notes.Unread, 0, "marked read")

	// Field change trigger.
	admin.must("POST", "/api/v1/projects/AUT/automation-rules", obj{
		"name": "Highest priority → selected", "trigger": obj{"type": "issue.field_changed", "field": "priority"},
		"condition": `issue.priority == "highest"`, "actions": []obj{{"type": "transition", "status": "Selected for Development"}},
	}, 201, nil)
	admin.must("PATCH", "/api/v1/issues/AUT-2", obj{"priority": "highest"}, 200, nil)
	e.DrainJobs()
	eq(t, str(admin.issue("AUT-2"), "status", "name"), "Selected for Development", "field_changed rule")

	// Two rules that trigger each other stop at the chain depth limit.
	var ping, pong obj
	admin.must("POST", "/api/v1/projects/AUT/automation-rules", obj{"name": "ping", "trigger": obj{"type": "issue.transitioned", "toStatus": "In Progress"},
		"actions": []obj{{"type": "transition", "status": "Backlog"}}}, 201, &ping)
	admin.must("POST", "/api/v1/projects/AUT/automation-rules", obj{"name": "pong", "trigger": obj{"type": "issue.transitioned", "toStatus": "Backlog"},
		"actions": []obj{{"type": "transition", "status": "In Progress"}}}, 201, &pong)
	admin.must("POST", "/api/v1/issues/AUT-2/move", obj{"statusId": admin.statusID("In Progress")}, 200, nil)
	e.DrainJobs()
	stopped := false
	for _, r := range append(runs(admin, str(ping, "id")), runs(admin, str(pong, "id"))...) {
		if strings.Contains(str(r, "message"), "depth") {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("automation loop was not stopped by the depth limit")
	}
	total := len(runs(admin, str(ping, "id"))) + len(runs(admin, str(pong, "id")))
	if total > e.Cfg.AutomationMaxDepth+2 {
		t.Fatalf("too many runs: %d", total)
	}
	admin.must("PUT", "/api/v1/automation-rules/"+str(ping, "id"), obj{"name": "ping", "enabled": false, "trigger": obj{"type": "issue.transitioned", "toStatus": "In Progress"},
		"actions": []obj{{"type": "transition", "status": "Backlog"}}}, 200, nil)
	admin.must("DELETE", "/api/v1/automation-rules/"+str(pong, "id"), nil, 204, nil)

	// Scheduled rules run over a JQL query.
	var sched obj
	admin.must("POST", "/api/v1/projects/AUT/automation-rules", obj{
		"name": "Flag stale", "trigger": obj{"type": "schedule", "cron": "0 9 * * 1", "jql": `labels = needs-triage`},
		"actions": []obj{{"type": "add_label", "label": "stale"}},
	}, 201, &sched)
	if sched["nextRunAt"] == nil {
		t.Fatal("scheduled rule has no next run")
	}
	e.exec(`UPDATE automation_rules SET next_run_at = now() - interval '1 minute' WHERE id = $1`, str(sched, "id"))
	if err := e.App.RunJob(context.Background(), "automation_schedule", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	e.DrainJobs()
	eq(t, fmt.Sprint(admin.issue("AUT-1")["labels"]), "[needs-triage stale]", "scheduled action")
	var rules []obj
	admin.get("/api/v1/projects/AUT/automation-rules", &rules)
	var after obj
	for _, r := range rules {
		if str(r, "id") == str(sched, "id") {
			after = r
		}
	}
	next, _ := time.Parse(time.RFC3339, str(after, "nextRunAt"))
	if !next.After(time.Now()) {
		t.Fatalf("next run not advanced: %v", after["nextRunAt"])
	}
}

func TestOutboundWebhooks(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	mel := e.login("mel@example.com", "Mel")
	admin.createProject("HK", "kanban")
	rcv := newReceiver(t)

	mel.must("POST", "/api/v1/webhooks", obj{"name": "x", "url": rcv.URL, "events": []string{"*"}}, 403, nil)                     // org-wide needs admin
	mel.must("POST", "/api/v1/webhooks", obj{"projectKey": "HK", "name": "x", "url": rcv.URL, "events": []string{"*"}}, 403, nil) // project admin
	admin.must("POST", "/api/v1/webhooks", obj{"projectKey": "HK", "name": "x", "url": "ftp://x", "events": []string{"*"}}, 400, nil)
	var hook obj
	admin.must("POST", "/api/v1/webhooks", obj{"projectKey": "HK", "name": "CI", "url": rcv.URL, "events": []string{"issue.created", "issue.transitioned", "comment.created"}}, 201, &hook)
	secret := str(hook, "secret")
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret = %q", secret)
	}

	admin.createIssue("HK", "task", "Hooked", nil)
	admin.must("PATCH", "/api/v1/issues/HK-1", obj{"summary": "Hooked (updated)"}, 200, nil) // not subscribed
	admin.transition("HK-1", "start-work", 200)
	admin.must("POST", "/api/v1/issues/HK-1/comments", obj{"body": "hi"}, 201, nil)
	e.DrainJobs()
	sameStrings(t, rcv.events(), []string{"issue.created", "issue.transitioned", "comment.created"}, "delivered events")

	// Payloads are signed with the subscription secret.
	d := rcv.got[0]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(d.Body)
	eq(t, d.Signature, "sha256="+hex.EncodeToString(mac.Sum(nil)), "signature")
	var payload obj
	_ = json.Unmarshal(d.Body, &payload)
	eq(t, str(payload, "issue", "key"), "HK-1", "issue in payload")
	eq(t, str(payload, "actor", "email"), "ada@example.com", "actor in payload")

	// Failed deliveries are recorded (and retried by the queue).
	rcv.mu.Lock()
	rcv.fail = true
	rcv.mu.Unlock()
	admin.must("POST", "/api/v1/webhooks/"+str(hook, "id")+"/test", nil, 202, nil)
	e.DrainJobs()
	var dels []obj
	admin.get("/api/v1/webhooks/"+str(hook, "id")+"/deliveries", &dels)
	eq(t, str(dels[0], "event"), "ping", "latest delivery")
	eq(t, str(dels[0], "status"), "failed", "failed delivery recorded")
	eq(t, fmt.Sprint(dels[0]["responseStatus"]), "503", "response status")

	// Automation webhook actions use the same signed delivery path.
	admin.must("POST", "/api/v1/projects/HK/automation-rules", obj{"name": "notify CI", "trigger": obj{"type": "issue.transitioned", "toStatus": "Done"},
		"actions": []obj{{"type": "webhook", "url": rcv.URL}}}, 201, nil)
	rcv.mu.Lock()
	rcv.fail, rcv.got = false, nil
	rcv.mu.Unlock()
	admin.transition("HK-1", "done", 200)
	e.DrainJobs()
	if !strings.Contains(strings.Join(rcv.events(), ","), "automation") {
		t.Fatalf("automation webhook not delivered: %v", rcv.events())
	}
}

func TestOutboundWebhooksBlockPrivateAddresses(t *testing.T) {
	e := newEnv(t, withEnv("WFM_WEBHOOK_ALLOW_PRIVATE", "false"))
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("SSRF", "kanban")
	rcv := newReceiver(t) // listens on 127.0.0.1
	var hook obj
	admin.must("POST", "/api/v1/webhooks", obj{"projectKey": "SSRF", "name": "internal", "url": rcv.URL, "events": []string{"issue.created"}}, 201, &hook)
	admin.createIssue("SSRF", "task", "x", nil)
	e.DrainJobs()
	eq(t, len(rcv.events()), 0, "no request reached the private address")
	var dels []obj
	admin.get("/api/v1/webhooks/"+str(hook, "id")+"/deliveries", &dels)
	eq(t, str(dels[0], "status"), "failed", "blocked delivery recorded")
	if !strings.Contains(str(dels[0], "lastError"), "not allowed") {
		t.Fatalf("lastError = %q", str(dels[0], "lastError"))
	}
}

func TestServerSentEvents(t *testing.T) {
	e := newEnv(t)
	admin := e.login("ada@example.com", "Ada")
	admin.createProject("LIVE", "kanban")
	outsider := e.login("out@example.com", "Out")
	admin.must("POST", "/api/v1/projects", obj{"key": "PRIV", "name": "Private", "template": "kanban", "access": "private"}, 201, nil)
	outsider.must("GET", "/api/v1/events?project=PRIV", nil, 404, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.Srv.URL+"/api/v1/events?project=LIVE", nil)
	res, err := admin.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	eq(t, res.Header.Get("Content-Type"), "text/event-stream", "content type")
	lines := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	waitFor := func(prefix string) string {
		t.Helper()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed waiting for %q", prefix)
				}
				if strings.HasPrefix(l, prefix) {
					return l
				}
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %q", prefix)
			}
		}
	}
	waitFor("event: ready")
	admin.createIssue("LIVE", "task", "Appears live", nil)
	waitFor("event: issue.created")
	data := waitFor("data: ")
	if !strings.Contains(data, "LIVE-1") {
		t.Fatalf("event data = %s", data)
	}
	admin.must("POST", "/api/v1/issues/LIVE-1/move", obj{"statusId": admin.statusID("Done")}, 200, nil)
	waitFor("event: issue.updated")
}

func TestAttachments(t *testing.T) {
	e := newEnv(t, withEnv("WFM_ATTACHMENT_MAX_MB", "1"))
	admin := e.login("ada@example.com", "Ada")
	viewer := e.login("vic@example.com", "Vic")
	admin.must("POST", "/api/v1/projects", obj{"key": "ATT", "name": "Attach", "template": "kanban", "access": "private"}, 201, nil)
	admin.must("POST", "/api/v1/projects/ATT/members", obj{"userId": viewer.id(), "roleKey": "viewer"}, 201, nil)
	admin.createIssue("ATT", "task", "With files", nil)

	upload := func(c *Client, name string, content []byte) Response {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(content)
		_ = mw.Close()
		return c.do("POST", "/api/v1/issues/ATT-1/attachments", buf.Bytes(), "Content-Type", mw.FormDataContentType())
	}
	r := upload(admin, "notes.txt", []byte("hello attachments"))
	eq(t, r.Status, 201, "upload: "+string(r.Body))
	att := r.JSON()
	eq(t, fmt.Sprint(att["size"]), "17", "size")
	eq(t, upload(viewer, "x.txt", []byte("x")).Status, 403, "viewers cannot upload")
	eq(t, upload(admin, "big.bin", bytes.Repeat([]byte("x"), 1<<20+1)).Status, 413, "size limit")

	dl := viewer.do("GET", "/api/v1/attachments/"+str(att, "id"), nil)
	eq(t, dl.Status, 200, "download")
	eq(t, string(dl.Body), "hello attachments", "content")
	if !strings.Contains(dl.Header.Get("Content-Disposition"), "notes.txt") {
		t.Fatalf("content disposition = %q", dl.Header.Get("Content-Disposition"))
	}
	eq(t, dl.Header.Get("X-Content-Type-Options"), "nosniff", "nosniff")
	outsider := e.login("out@example.com", "Out")
	outsider.must("GET", "/api/v1/attachments/"+str(att, "id"), nil, 404, nil)

	viewer.must("DELETE", "/api/v1/attachments/"+str(att, "id"), nil, 403, nil)
	admin.must("DELETE", "/api/v1/attachments/"+str(att, "id"), nil, 204, nil)
	admin.must("GET", "/api/v1/attachments/"+str(att, "id"), nil, 404, nil)
}
