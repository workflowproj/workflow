package integration

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/workflowproj/workflow/internal/mail"
)

// mailbox returns the emails the in-memory sender holds for an address.
func (e *Env) mailbox(to string) []mail.Sent {
	e.t.Helper()
	box, ok := e.App.Mailer.(*mail.Memory)
	if !ok {
		e.t.Fatal("mailer is not the in-memory mailbox")
	}
	return box.Messages(to)
}

// flushEmails runs the background jobs, including immediate-email jobs scheduled a batch
// window ahead (as if the window had passed).
func (e *Env) flushEmails() {
	e.t.Helper()
	e.DrainJobs()
	e.exec(`UPDATE river_job SET state = 'available', scheduled_at = now() WHERE state = 'scheduled' AND kind = 'email_notify'`)
	e.DrainJobs()
}

func emailSettings(c *Client) obj {
	var s obj
	c.get("/api/v1/me/email-notifications", &s)
	return s
}

func deliveryOf(s obj, kind string) string {
	for _, p := range s["preferences"].([]any) {
		if m := p.(map[string]any); m["kind"] == kind {
			return m["delivery"].(string)
		}
	}
	return ""
}

func TestEmailNotifications(t *testing.T) {
	e := newEnv(t)
	ada := e.login("ada@example.com", "Ada Admin")
	mel := e.login("mel@example.com", "Mel Member")
	bob := e.login("bob@example.com", "Bob Watcher")
	ada.createProject("EML", "kanban")

	// Defaults: assignments immediately, the rest in a 09:00 UTC digest.
	s := emailSettings(mel)
	eq(t, deliveryOf(s, "assigned"), "immediate", "assigned default")
	eq(t, deliveryOf(s, "commented"), "digest", "commented default")
	eq(t, deliveryOf(s, "transitioned"), "digest", "transitioned default")
	eq(t, s["timeZone"].(string), "UTC", "default zone")
	eq(t, s["digestHour"].(float64), 9.0, "default hour")
	eq(t, s["address"].(string), "mel@example.com", "address")

	// Validation.
	mel.must("PATCH", "/api/v1/me/email-notifications", obj{"timeZone": "Mars/Olympus"}, 400, nil)
	mel.must("PATCH", "/api/v1/me/email-notifications", obj{"timeZone": "Local"}, 400, nil)
	mel.must("PATCH", "/api/v1/me/email-notifications", obj{"digestHour": 24}, 400, nil)
	mel.must("PATCH", "/api/v1/me/email-notifications", obj{"preferences": []obj{{"kind": "nope", "delivery": "off"}}}, 400, nil)

	// Mel wants comments and status changes right away too.
	mel.must("PATCH", "/api/v1/me/email-notifications", obj{"preferences": []obj{
		{"kind": "commented", "delivery": "immediate"}, {"kind": "transitioned", "delivery": "immediate"},
	}, "timeZone": "Europe/London"}, 200, &s)
	eq(t, deliveryOf(s, "commented"), "immediate", "saved")
	eq(t, deliveryOf(s, "assigned"), "immediate", "others unchanged")
	eq(t, s["timeZone"].(string), "Europe/London", "zone saved")

	// A burst of changes by Ada: assign, comment, move. Mel gets one email once the window passes.
	ada.createIssue("EML", "task", "Fix the <login> redirect", obj{"assigneeId": mel.id()})
	ada.must("POST", "/api/v1/issues/EML-1/comments", obj{"body": "Found it: the **callback** URL.\n<script>alert(1)</script>"}, 201, nil)
	var trs []obj
	ada.get("/api/v1/issues/EML-1/transitions", &trs)
	ada.transition("EML-1", str(trs[0], "id"), 200)
	e.DrainJobs()
	if n := len(e.mailbox("mel@example.com")); n != 0 {
		t.Fatalf("emails went out before the batch window: %d", n)
	}
	eq(t, e.JobsOfKind("email_notify"), 1, "one coalescing job per person and issue")
	e.flushEmails()
	msgs := e.mailbox("mel@example.com")
	eq(t, len(msgs), 1, "one email for the burst")
	m := msgs[0]
	eq(t, m.Subject, "[EML-1] Fix the <login> redirect", "subject")
	for _, want := range []string{"3 updates on EML-1", "Ada Admin", "Assigned this issue to you", "Found it: the **callback** URL.", "Moved from", "/browse/EML-1", "you're assigned to EML-1"} {
		if !strings.Contains(m.Text, want) {
			t.Fatalf("text lacks %q:\n%s", want, m.Text)
		}
	}
	if strings.Contains(m.HTML, "<script>") || strings.Contains(m.HTML, "<login>") || !strings.Contains(m.HTML, "&lt;script&gt;") {
		t.Fatalf("HTML not escaped:\n%s", m.HTML)
	}
	unsub := m.Headers["List-Unsubscribe"]
	if !strings.HasPrefix(unsub, "<"+e.Cfg.HooksPublicURL+"/hooks/email/unsubscribe?t=") || m.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Fatalf("unsubscribe headers: %v", m.Headers)
	}
	if m.Headers["Auto-Submitted"] != "auto-generated" || !strings.Contains(m.Headers["References"], "@") {
		t.Fatalf("headers: %v", m.Headers)
	}
	// Never about your own changes.
	eq(t, len(e.mailbox("ada@example.com")), 0, "no email to the actor")

	// Bob watches; comments go to his digest, not right away.
	bob.must("POST", "/api/v1/issues/EML-1/watchers", obj{}, 204, nil)
	ada.must("POST", "/api/v1/issues/EML-1/comments", obj{"body": "Second thought"}, 201, nil)
	e.flushEmails()
	eq(t, len(e.mailbox("mel@example.com")), 2, "mel: immediate again")
	eq(t, len(e.mailbox("bob@example.com")), 0, "bob: waits for the digest")
	runDigests := func() {
		t.Helper()
		if err := e.App.RunJob(context.Background(), "email_digests", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		e.DrainJobs()
	}
	// Not due: no digest time has passed since the notification arrived.
	bob.must("PATCH", "/api/v1/me/email-notifications", obj{"digestHour": 23, "timeZone": "Pacific/Kiritimati"}, 200, nil)
	runDigests()
	eq(t, len(e.mailbox("bob@example.com")), 0, "digest not due yet")
	// Back-date the notification past a digest time: now it is due.
	e.exec(`UPDATE notifications SET created_at = now() - interval '2 days' WHERE user_id = $1`, bob.id())
	runDigests()
	digests := e.mailbox("bob@example.com")
	eq(t, len(digests), 1, "digest sent")
	d := digests[0]
	eq(t, d.Subject, "Daily digest: 1 update on 1 issue", "digest subject")
	for _, want := range []string{"EML-1", "Second thought", "Times are in Pacific/Kiritimati", "Stop the daily digest"} {
		if !strings.Contains(d.Text, want) {
			t.Fatalf("digest lacks %q:\n%s", want, d.Text)
		}
	}
	if emailSettings(bob)["lastDigestAt"] == nil {
		t.Fatal("last digest time not recorded")
	}
	runDigests()
	eq(t, len(e.mailbox("bob@example.com")), 1, "nothing new, no second digest")

	// One-click unsubscribe from the digest (RFC 8058) switches the digest kinds off.
	u, _ := url.Parse(strings.Trim(d.Headers["List-Unsubscribe"], "<>"))
	path := u.RequestURI()
	anon := e.anon()
	anon.csrf = false
	page := anon.do("GET", path, nil)
	eq(t, page.Status, 200, "confirmation page")
	if !strings.Contains(string(page.Body), "Unsubscribe from the daily digest?") {
		t.Fatalf("page: %s", page.Body)
	}
	eq(t, deliveryOf(emailSettings(bob), "commented"), "digest", "GET changes nothing")
	r := anon.do("POST", path, "List-Unsubscribe=One-Click", "Content-Type", "application/x-www-form-urlencoded")
	eq(t, r.Status, 200, "one-click")
	bs := emailSettings(bob)
	eq(t, deliveryOf(bs, "commented"), "off", "digest kinds off")
	eq(t, deliveryOf(bs, "assigned"), "immediate", "immediate kinds untouched")
	// A tampered token is refused.
	eq(t, anon.do("POST", path[:len(path)-3]+"xyz", "List-Unsubscribe=One-Click", "Content-Type", "application/x-www-form-urlencoded").Status, 400, "tampered")

	// The confirmation page can also pause everything.
	mu, _ := url.Parse(strings.Trim(m.Headers["List-Unsubscribe"], "<>"))
	page = anon.do("GET", mu.RequestURI(), nil)
	if !strings.Contains(string(page.Body), "Unsubscribe from emails about assignments, comments and status changes?") {
		t.Fatalf("page: %s", page.Body)
	}
	r = anon.do("POST", mu.RequestURI(), "action=pause", "Content-Type", "application/x-www-form-urlencoded")
	if r.Status != 200 || !strings.Contains(string(r.Body), "paused") {
		t.Fatalf("pause: %d %s", r.Status, r.Body)
	}
	if emailSettings(mel)["paused"] != true {
		t.Fatal("not paused")
	}
	ada.must("POST", "/api/v1/issues/EML-1/comments", obj{"body": "While paused"}, 201, nil)
	e.flushEmails()
	eq(t, len(e.mailbox("mel@example.com")), 2, "paused: no email")
	mel.must("PATCH", "/api/v1/me/email-notifications", obj{"paused": false}, 200, nil)

	// Only people who can still browse the project: a change, then the project goes private.
	ada.must("POST", "/api/v1/issues/EML-1/comments", obj{"body": "Mel will not see this"}, 201, nil)
	e.DrainJobs()
	ada.must("PATCH", "/api/v1/projects/EML", obj{"access": "private"}, 200, nil)
	e.flushEmails()
	eq(t, len(e.mailbox("mel@example.com")), 2, "no access: no email")
	ada.must("PATCH", "/api/v1/projects/EML", obj{"access": "open"}, 200, nil)

	// Deactivated people get nothing.
	ada.must("PATCH", "/api/v1/admin/users/"+mel.id(), obj{"active": false}, 200, nil)
	ada.must("POST", "/api/v1/issues/EML-1/comments", obj{"body": "Mel has left"}, 201, nil)
	e.flushEmails()
	eq(t, len(e.mailbox("mel@example.com")), 2, "deactivated: no email")
	var pendingLeft int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM notifications WHERE email_delivery IS NOT NULL AND emailed_at IS NULL`).Scan(&pendingLeft); err != nil {
		t.Fatal(err)
	}
	eq(t, pendingLeft, 0, "everything sent or skipped")

	// Admin: configuration and a test email (sent at once, to the caller).
	var status obj
	ada.get("/api/v1/admin/email", &status)
	eq(t, str(status, "transport"), "memory", "no SMTP configured")
	eq(t, str(status, "unsubscribeUrl"), e.Cfg.HooksPublicURL+"/hooks/email/unsubscribe", "unsubscribe url")
	bob.must("POST", "/api/v1/admin/email/test", nil, 403, nil)
	var res obj
	ada.must("POST", "/api/v1/admin/email/test", nil, 200, &res)
	eq(t, str(res, "to"), "ada@example.com", "test email to the caller")
	adaMail := e.mailbox("ada@example.com")
	if len(adaMail) != 1 || !strings.Contains(adaMail[0].Subject, "Test email") {
		t.Fatalf("test email: %+v", adaMail)
	}

	// The development mailbox also lists queued mail another process (a separate worker) sent,
	// once each.
	var listed []obj
	ada.get("/api/v1/dev/mailbox?to=mel@example.com", &listed)
	eq(t, len(listed), 2, "sent in this process: listed once")
	e.exec(`INSERT INTO river_job (kind, args, max_attempts, queue, state, finalized_at)
		VALUES ('email_send', '{"to": ["mel@example.com"], "subject": "From the worker", "text": "hi"}', 1, 'outbound', 'completed', now())`)
	ada.get("/api/v1/dev/mailbox?to=mel@example.com", &listed)
	eq(t, len(listed), 3, "sent by another process")
	eq(t, str(listed[2], "subject"), "From the worker", "newest last")
}

// TestEmailUnsubscribeBadToken checks the unsubscribe page without a valid token.
func TestEmailUnsubscribeBadToken(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	r := c.do("GET", "/hooks/email/unsubscribe?t=forged.token", nil)
	eq(t, r.Status, http.StatusBadRequest, "forged token")
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("csp: %v", r.Header)
	}
}
