package notify

import (
	"context"
	"encoding/base64"
	"errors"
	htmltemplate "html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UnsubscribePath serves one-click unsubscribe links. It lives under /hooks so that it is
// reachable on the internet-facing hooks ingress: mail providers POST to it (RFC 8058) from
// outside the network, and people follow it from any device.
const UnsubscribePath = "/hooks/email/unsubscribe"

const tokenPrefix = "unsubscribe:"

// Unsubscribe scopes: "digest" (the daily digest) or "kinds:a,b" (those kinds' emails).
const scopeDigest = "digest"

// UnsubscribeURL returns a signed link that unsubscribes one person from a scope. The token
// is an HMAC (WFM_SECRET_KEY) over the person and scope, so it cannot be guessed or altered.
func (s *Service) UnsubscribeURL(userID uuid.UUID, scope string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(tokenPrefix + userID.String() + ":" + scope))
	return s.Cfg.HooksPublicURL + UnsubscribePath + "?t=" + url.QueryEscape(s.Box.Sign(payload))
}

type unsubToken struct {
	UserID uuid.UUID
	Scope  string
}

func (s *Service) parseToken(t string) (unsubToken, bool) {
	payload, ok := s.Box.Verify(t)
	if !ok {
		return unsubToken{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || !strings.HasPrefix(string(raw), tokenPrefix) {
		return unsubToken{}, false
	}
	id, scope, _ := strings.Cut(strings.TrimPrefix(string(raw), tokenPrefix), ":")
	uid, err := uuid.Parse(id)
	if err != nil {
		return unsubToken{}, false
	}
	return unsubToken{UserID: uid, Scope: scope}, true
}

// scopeKinds lists the kinds a scope switches off, given the current settings.
func scopeKinds(scope string, st Settings) []string {
	var out []string
	if scope == scopeDigest {
		for _, k := range Kinds {
			if st.Delivery(k.Key) == Digest {
				out = append(out, k.Key)
			}
		}
		return out
	}
	for _, k := range strings.Split(strings.TrimPrefix(scope, "kinds:"), ",") {
		if _, ok := KindByKey(k); ok {
			out = append(out, k)
		}
	}
	return out
}

// unsubscribe applies a token: switches the scope's kinds off, or pauses all email.
func (s *Service) unsubscribe(ctx context.Context, tok unsubToken, pauseAll bool) error {
	if pauseAll {
		paused := true
		_, err := s.Update(ctx, tok.UserID, Patch{Paused: &paused})
		return err
	}
	st, err := s.Get(ctx, tok.UserID)
	if err != nil {
		return err
	}
	p := Patch{Deliveries: map[string]string{}}
	for _, k := range scopeKinds(tok.Scope, st) {
		p.Deliveries[k] = Off
	}
	_, err = s.Update(ctx, tok.UserID, p)
	return err
}

// scopeLabel describes a scope: "the daily digest", "emails about comments".
func scopeLabel(scope string) string {
	if scope == scopeDigest {
		return "the daily digest"
	}
	var kinds []string
	for _, k := range strings.Split(strings.TrimPrefix(scope, "kinds:"), ",") {
		if _, ok := KindByKey(k); ok {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return "these emails"
	}
	return "e" + strings.TrimPrefix(stopLabel(kinds), "Stop e")
}

// HandleUnsubscribe serves UnsubscribePath. GET shows a confirmation page (link scanners must
// not unsubscribe anyone); POST unsubscribes, either from that page or as an RFC 8058
// one-click request ("List-Unsubscribe=One-Click") from a mail provider.
func (s *Service) HandleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.Method == http.MethodPost {
		_ = r.ParseMultipartForm(64 << 10) // #nosec G120 -- the body is capped by MaxBytesReader above; also parses urlencoded bodies
	}
	tok, ok := s.parseToken(r.FormValue("t"))
	if !ok {
		s.page(w, http.StatusBadRequest, unsubPage{Title: "This link doesn't work", Text: "The unsubscribe link is incomplete or was changed. Use the link in a recent email, or change your email notifications in your profile."})
		return
	}
	u, err := s.Q.GetUser(r.Context(), tok.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		s.page(w, http.StatusNotFound, unsubPage{Title: "Account not found", Text: "The account this link was sent to no longer exists."})
		return
	} else if err != nil {
		s.Log.Error("unsubscribe", "error", err)
		s.page(w, http.StatusInternalServerError, unsubPage{Title: "Something went wrong", Text: "Please try again in a moment."})
		return
	}
	what := scopeLabel(tok.Scope)
	if r.Method != http.MethodPost {
		s.page(w, http.StatusOK, unsubPage{Title: "Unsubscribe from " + what + "?", Text: "We'll stop sending " + what + " to " + u.Email + ". In-app notifications continue.", Confirm: true})
		return
	}
	oneClick := r.PostFormValue("List-Unsubscribe") == "One-Click"
	pauseAll := r.PostFormValue("action") == "pause"
	if err := s.unsubscribe(r.Context(), tok, pauseAll); err != nil {
		s.Log.Error("unsubscribe", "error", err)
		s.page(w, http.StatusInternalServerError, unsubPage{Title: "Something went wrong", Text: "Please try again in a moment."})
		return
	}
	s.Log.Info("email unsubscribe", "user", tok.UserID, "scope", tok.Scope, "pause_all", pauseAll, "one_click", oneClick)
	if oneClick {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Unsubscribed.\n"))
		return
	}
	if pauseAll {
		s.page(w, http.StatusOK, unsubPage{Title: "Email notifications paused", Text: "We won't email " + u.Email + " about notifications until you turn them back on. In-app notifications continue.", Done: true})
		return
	}
	s.page(w, http.StatusOK, unsubPage{Title: "You're unsubscribed", Text: "We won't send " + what + " to " + u.Email + " any more. In-app notifications continue.", Done: true})
}

type unsubPage struct {
	Brand, Title, Text string
	Confirm, Done      bool
	ManageURL          string
}

func (s *Service) page(w http.ResponseWriter, status int, p unsubPage) {
	p.Brand, p.ManageURL = s.Brand(), s.ManageURL()
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if err := pageTmpl.Execute(w, p); err != nil {
		s.Log.Warn("unsubscribe page", "error", err)
	}
}

var pageTmpl = htmltemplate.Must(htmltemplate.New("unsubscribe").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}} · {{.Brand}}</title>
<style>
  :root { color-scheme: light dark; --bg: #f6f7f9; --card: #fff; --line: #e2e5eb; --ink: #171a21; --muted: #5d6573; --accent: #2563eb; --accent-ink: #fff; --soft: #f1f3f6; }
  @media (prefers-color-scheme: dark) { :root { --bg: #0f1115; --card: #171a20; --line: #2a2f39; --ink: #e8eaee; --muted: #a3aab6; --accent: #4c8dff; --accent-ink: #0b1020; --soft: #1e222a; } }
  * { box-sizing: border-box; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px 16px; background: var(--bg); color: var(--ink);
         font: 14px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
  main { width: 100%; max-width: 440px; }
  .brand { margin: 0 0 12px 4px; font-size: 13px; font-weight: 600; color: var(--muted); }
  .card { background: var(--card); border: 1px solid var(--line); border-radius: 8px; padding: 24px; }
  h1 { margin: 0; font-size: 18px; line-height: 1.4; font-weight: 600; }
  p { margin: 8px 0 0; color: var(--muted); }
  form { margin: 20px 0 0; display: flex; flex-wrap: wrap; gap: 8px; }
  button { font: inherit; font-weight: 600; border-radius: 6px; padding: 8px 14px; cursor: pointer; border: 1px solid var(--line); background: var(--soft); color: var(--ink); }
  button.primary { background: var(--accent); border-color: var(--accent); color: var(--accent-ink); }
  button:focus-visible, a:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  a { color: var(--accent); }
  .foot { margin: 16px 4px 0; font-size: 13px; }
</style>
</head>
<body>
<main>
  <p class="brand">{{.Brand}}</p>
  <div class="card">
    <h1>{{.Title}}</h1>
    <p>{{.Text}}</p>
    {{- if .Confirm}}
    <form method="post">
      <button class="primary" type="submit" name="action" value="unsubscribe">Unsubscribe</button>
      <button type="submit" name="action" value="pause">Pause all notification emails</button>
    </form>
    {{- end}}
  </div>
  <p class="foot"><a href="{{.ManageURL}}">Manage email notifications</a>{{if .Done}} — you can turn emails back on there at any time.{{end}}</p>
</main>
</body>
</html>
`))
