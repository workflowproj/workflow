package notify

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

func ptr[T any](v T) *T { return &v }

func TestDeliveryFor(t *testing.T) {
	person := dbq.EmailRecipientRow{Email: "a@example.com", Kind: "human", Active: true}
	cases := []struct {
		name  string
		r     func(dbq.EmailRecipientRow) dbq.EmailRecipientRow
		kind  string
		issue bool
		want  string
	}{
		{"assignments default to immediate", nil, "assigned", true, Immediate},
		{"comments default to the digest", nil, "commented", true, Digest},
		{"unknown kinds go to the digest", nil, "mentioned", true, Digest},
		{"no issue, no email", nil, "assigned", false, ""},
		{"override", func(r dbq.EmailRecipientRow) dbq.EmailRecipientRow {
			r.Deliveries = []byte(`{"assigned":"off","commented":"immediate"}`)
			return r
		}, "assigned", true, ""},
		{"override to immediate", func(r dbq.EmailRecipientRow) dbq.EmailRecipientRow {
			r.Deliveries = []byte(`{"commented":"immediate"}`)
			return r
		}, "commented", true, Immediate},
		{"paused", func(r dbq.EmailRecipientRow) dbq.EmailRecipientRow { r.Paused = ptr(true); return r }, "assigned", true, ""},
		{"service account", func(r dbq.EmailRecipientRow) dbq.EmailRecipientRow { r.Kind = "service"; return r }, "assigned", true, ""},
		{"deactivated", func(r dbq.EmailRecipientRow) dbq.EmailRecipientRow { r.Active = false; return r }, "assigned", true, ""},
		{"deleted", func(r dbq.EmailRecipientRow) dbq.EmailRecipientRow { r.DeletedAt = ptr(time.Now()); return r }, "assigned", true, ""},
	}
	for _, c := range cases {
		r := person
		if c.r != nil {
			r = c.r(r)
		}
		if got := DeliveryFor(r, c.kind, c.issue); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDigestTimes(t *testing.T) {
	s := Settings{DigestHour: 9, TimeZone: "Europe/London"}
	now := time.Date(2026, 10, 24, 8, 30, 0, 0, time.UTC) // 09:30 in London (summer time)
	if got, want := s.digestSlot(now), time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("slot: got %v, want %v", got, want)
	}
	// Summer time ends overnight, so tomorrow's 09:00 is 09:00 UTC.
	if got, want := s.NextDigest(now), time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("next: got %v, want %v", got, want)
	}
	if !s.DigestDue(now.Add(-31*time.Minute), now) {
		t.Fatal("a notification from before 09:00 is due")
	}
	if s.DigestDue(now.Add(-29*time.Minute), now) {
		t.Fatal("a notification from after 09:00 waits for tomorrow")
	}
	// Before today's hour, the slot is yesterday's.
	early := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	if got, want := s.digestSlot(early), time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("early slot: got %v, want %v", got, want)
	}
	// An unknown zone falls back to UTC rather than failing.
	bad := Settings{DigestHour: 9, TimeZone: "Nowhere/Special"}
	if bad.Location() != time.UTC {
		t.Fatal("fallback to UTC")
	}
	if _, err := LoadZone("Local"); err == nil {
		t.Fatal("Local is refused")
	}
}

func TestSettingsDelivery(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"transitioned": Off, "automation": "bogus"})
	s := FromRow(dbq.EmailNotificationSetting{Deliveries: raw, DigestHour: 7, TimeZone: "UTC"})
	if s.Delivery("transitioned") != Off || s.Delivery("automation") != Digest || s.Delivery("assigned") != Immediate {
		t.Fatalf("deliveries: %+v", s.Deliveries)
	}
}

func testService(t *testing.T) *Service {
	box, err := secure.NewBox("unit-test-secret-key-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Box: box, Cfg: &config.Config{PublicURL: "https://wfm.example.com", HooksPublicURL: "https://hooks.example.com", SMTP: config.SMTP{From: "Acme Tracker <wfm@example.com>"}}}
}

func TestUnsubscribeToken(t *testing.T) {
	s := testService(t)
	uid := uuid.New()
	link := s.UnsubscribeURL(uid, "kinds:commented,transitioned")
	if !strings.HasPrefix(link, "https://hooks.example.com/hooks/email/unsubscribe?t=") {
		t.Fatalf("link: %s", link)
	}
	u, _ := url.Parse(link)
	tok := u.Query().Get("t")
	got, ok := s.parseToken(tok)
	if !ok || got.UserID != uid || got.Scope != "kinds:commented,transitioned" {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
	// Altered or foreign tokens are refused.
	for _, bad := range []string{"", tok + "x", strings.Replace(tok, tok[:4], "AAAA", 1), s.Box.Sign("hello"), testServiceOther(t).Box.Sign(strings.Split(tok, ".")[0])} {
		if _, ok := s.parseToken(bad); ok {
			t.Fatalf("accepted %q", bad)
		}
	}
	// Scopes.
	st := Defaults()
	st.Deliveries["automation"] = Off
	if got := strings.Join(scopeKinds("digest", st), ","); got != "commented,transitioned" {
		t.Fatalf("digest kinds: %s", got)
	}
	if got := strings.Join(scopeKinds("kinds:assigned,nope", st), ","); got != "assigned" {
		t.Fatalf("kinds: %s", got)
	}
	if got := scopeLabel("kinds:commented,transitioned"); got != "emails about comments and status changes" {
		t.Fatalf("label: %s", got)
	}
}

func testServiceOther(t *testing.T) *Service {
	box, err := secure.NewBox("another-secret-key-0123456789abcdef!!")
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Box: box}
}

func TestRender(t *testing.T) {
	s := testService(t)
	e := Email{
		Brand: s.Brand(), Subject: "[PLAT-1] <b>Bold</b> & co", Headline: "Ana commented on PLAT-1",
		Issues: []IssueBlock{{
			Key: "PLAT-1", Summary: "<b>Bold</b> & co", URL: "https://wfm.example.com/browse/PLAT-1", Status: "In Progress", StatusCategory: "in_progress",
			Events: []Event{{Actor: "Ana", When: "14:05", What: "Commented", Excerpt: "line one\n<script>alert(1)</script>"}},
		}},
		Button: &Link{Label: "View PLAT-1", URL: "https://wfm.example.com/browse/PLAT-1"},
		Reason: "You're receiving this because you're watching PLAT-1.", ManageURL: s.ManageURL(),
		Unsub: &Link{Label: stopLabel([]string{"commented"}), URL: s.UnsubscribeURL(uuid.New(), "kinds:commented")},
	}
	text, html, err := e.Render()
	if err != nil {
		t.Fatal(err)
	}
	if e.Brand != "Acme Tracker" {
		t.Fatalf("brand from WFM_SMTP_FROM: %q", e.Brand)
	}
	for _, bad := range []string{"<script>", "<b>Bold</b>"} {
		if strings.Contains(html, bad) {
			t.Fatalf("html contains %q", bad)
		}
	}
	for _, want := range []string{"&lt;script&gt;", "line one<br>", "View PLAT-1", "Stop emails about comments", "Manage email notifications", "https://wfm.example.com/profile#email-notifications", "background-color:#e3ecfe"} {
		if !strings.Contains(html, want) {
			t.Fatalf("html lacks %q", want)
		}
	}
	for _, want := range []string{"PLAT-1: <b>Bold</b> & co", "> line one\n> <script>alert(1)</script>", "\n-- \n", "View PLAT-1: https://wfm.example.com/browse/PLAT-1", "Stop emails about comments: https://hooks.example.com/"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text lacks %q:\n%s", want, text)
		}
	}
}

func TestInlineMarkdown(t *testing.T) {
	cases := map[string]string{
		"plain <b>text</b> & more":           "plain &lt;b&gt;text&lt;/b&gt; &amp; more",
		"use `a<b` and **bold <i>**":         `use <code style="font-family:SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;">a&lt;b</code> and <strong>bold &lt;i&gt;</strong>`,
		"see [the PR](https://x.io/p?a=1&b)": `see <a href="https://x.io/p?a=1&amp;b" class="wfm-link" style="color:#2563eb;">the PR</a>`,
		"at https://x.io/a.":                 `at <a href="https://x.io/a" class="wfm-link" style="color:#2563eb;">https://x.io/a</a>.`,
		"[bad](javascript:alert(1))":         "[bad](javascript:alert(1))",
		`[q](https://x.io/" onclick="y)`:     `[q](<a href="https://x.io/" class="wfm-link" style="color:#2563eb;">https://x.io/</a>&#34; onclick=&#34;y)`,
		"**unclosed bold":                    "**unclosed bold",
	}
	for in, want := range cases {
		if got := string(inlineMarkdown(in)); got != want {
			t.Errorf("%q:\n got %s\nwant %s", in, got, want)
		}
	}
}

func TestExcerpt(t *testing.T) {
	if got := Excerpt("  a\r\n\n\n\nb  ", 50); got != "a\n\nb" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("word ", 40)
	got := Excerpt(long, 50)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) > 51 || strings.HasSuffix(got, " …") {
		t.Fatalf("got %q", got)
	}
	if got := stopLabel([]string{"assigned", "commented", "transitioned"}); got != "Stop emails about assignments, comments and status changes" {
		t.Fatalf("label %q", got)
	}
}
