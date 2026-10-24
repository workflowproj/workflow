package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/config"
	"github.com/geoffo-dev/workflow-management/internal/netguard"
	"github.com/geoffo-dev/workflow-management/internal/secure"
)

func testServer(t *testing.T, public string) *Server {
	t.Helper()
	cfg, err := config.LoadFrom(func(k string) string {
		return map[string]string{"WFM_ENV": "development", "WFM_PUBLIC_URL": public}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	box, err := secure.NewBox(cfg.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, nil, nil, box, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestCheckRedirectURI(t *testing.T) {
	for _, ok := range []string{
		"https://claude.ai/api/mcp/auth_callback",
		"https://app.example:8443/cb?x=1",
		"http://127.0.0.1/callback",
		"http://127.0.0.1:33418",
		"http://[::1]:8080/cb",
		"http://localhost:3118/callback",
		"cursor://anysphere.cursor-mcp/oauth/callback",
		"com.example.app:/oauth2redirect",
	} {
		if err := checkRedirectURI(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "http://evil.example/cb", "http://10.0.0.1/cb", "https://app.example/cb#x", "https://u:p@app.example/cb",
		"javascript:alert(1)", "JavaScript:alert(1)", "data:text/html,hi", "file:///etc/passwd", "vbscript:x", "ws://127.0.0.1/x",
		"https:///nohost", "/relative/cb", "https://app.example/c b", "https://app.example/\ncb", "http://localhost.evil.example/cb",
		"http://127.0.0.1.nip.io/cb",
	} {
		if err := checkRedirectURI(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRedirectMatches(t *testing.T) {
	reg := []string{"https://claude.ai/api/mcp/auth_callback", "http://127.0.0.1/callback", "http://localhost:3000/cb?app=1", "cursor://anysphere.cursor-mcp/oauth/callback"}
	for req, want := range map[string]bool{
		"https://claude.ai/api/mcp/auth_callback":       true,
		"https://claude.ai/api/mcp/auth_callback/":      false,
		"https://claude.ai/api/mcp/auth_callback?x=1":   false,
		"https://CLAUDE.ai/api/mcp/auth_callback":       false,
		"http://127.0.0.1:55123/callback":               true, // any loopback port (RFC 8252 §7.3)
		"http://127.0.0.1/callback":                     true,
		"http://127.0.0.1:55123/callback2":              false,
		"http://localhost:55123/callback":               false, // localhost is not 127.0.0.1
		"http://localhost:4000/cb?app=1":                true,
		"http://localhost:4000/cb?app=2":                false,
		"http://127.0.0.1:55123/callback#x":             false,
		"https://127.0.0.1:55123/callback":              false,
		"cursor://anysphere.cursor-mcp/oauth/callback":  true,
		"cursor://anysphere.cursor-mcp/oauth/callback2": false,
	} {
		if got := redirectMatches(reg, req); got != want {
			t.Errorf("%s: got %v, want %v", req, got, want)
		}
	}
}

func TestScopes(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "mcp:read mcp:write",
		"offline_access openid":        "mcp:read mcp:write",
		"mcp:read":                     "mcp:read",
		"mcp:write":                    "mcp:read mcp:write",
		"mcp:read offline_access":      "mcp:read",
		"  mcp:write   mcp:read  ":     "mcp:read mcp:write",
		"mcp:read mcp:write mcp:admin": "mcp:read mcp:write",
	} {
		if got := strings.Join(parseScopes(in), " "); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := ActorScopes([]string{"mcp:read"}); strings.Join(got, ",") != "read" {
		t.Errorf("read: %v", got)
	}
	if got := ActorScopes([]string{"mcp:read", "mcp:write"}); strings.Join(got, ",") != "read,write" {
		t.Errorf("write: %v", got)
	}
}

func TestResource(t *testing.T) {
	s := testServer(t, "https://WFM.example.com")
	if s.Resource() != "https://wfm.example.com/mcp" {
		t.Fatalf("resource %s", s.Resource())
	}
	for res, want := range map[string]bool{
		"https://wfm.example.com/mcp":      true,
		"https://wfm.example.com/mcp/":     true,
		"HTTPS://WFM.EXAMPLE.COM/mcp":      true,
		"https://wfm.example.com:443/mcp":  true,
		"https://wfm.example.com":          false,
		"https://wfm.example.com/MCP":      false,
		"https://wfm.example.com/mcp#x":    false,
		"https://other.example.com/mcp":    false,
		"http://wfm.example.com/mcp":       false,
		"https://wfm.example.com:8443/mcp": false,
		"wfm.example.com/mcp":              false,
	} {
		if got := s.resourceMatches(res); got != want {
			t.Errorf("%s: got %v, want %v", res, got, want)
		}
	}
}

func TestPendingRequestIsSealed(t *testing.T) {
	s := testServer(t, "http://localhost:8080")
	p := pending{ClientID: "wfmc_x", Redirect: "https://app.example/cb", State: "s", Scopes: []string{ScopeRead}, Expires: time.Now().Add(time.Minute).Unix()}
	sealed := s.seal(p)
	if strings.Contains(sealed, "app.example") {
		t.Fatal("pending requests are encrypted")
	}
	got, err := s.open(sealed)
	if err != nil || got.Redirect != p.Redirect {
		t.Fatalf("open: %+v %v", got, err)
	}
	if _, err := s.open(sealed[:len(sealed)-2] + "AA"); err == nil {
		t.Fatal("tampered request accepted")
	}
	p.Expires = time.Now().Add(-time.Second).Unix()
	if _, err := s.open(s.seal(p)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired request: %v", err)
	}
	// Other sealed values (e.g. stored secrets) are not authorisation requests.
	if _, err := s.open(s.Box.Seal([]byte(`{"c":"wfmc_x","e":99999999999}`))); err == nil {
		t.Fatal("foreign sealed value accepted")
	}
}

func TestCacheLifetime(t *testing.T) {
	for cc, want := range map[string]time.Duration{
		"":                       time.Hour,
		"public, max-age=300":    5 * time.Minute,
		"max-age=60":             5 * time.Minute,
		"max-age=604800":         24 * time.Hour,
		"no-store":               5 * time.Minute,
		"private, max-age=7200":  2 * time.Hour,
		"max-age=garbage, other": time.Hour,
	} {
		h := http.Header{}
		h.Set("Cache-Control", cc)
		if got := cacheLifetime(h); got != want {
			t.Errorf("%q: got %v, want %v", cc, got, want)
		}
	}
}

func TestFetchMetadata(t *testing.T) {
	var body string
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect.json" {
			http.Redirect(w, r, "/client.json", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	defer site.Close()
	s := testServer(t, "http://localhost:8080")
	id := site.URL + "/client.json"
	doc := func(extra string) string {
		return fmt.Sprintf(`{"client_id":%q,"client_name":"App","redirect_uris":["https://app.example/cb"]%s}`, id, extra)
	}

	// The default client refuses loopback and private addresses (SSRF), and never follows redirects.
	body = doc("")
	if _, _, err := s.fetchMetadata(context.Background(), id); err == nil || !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("loopback fetch must be blocked: %v", err)
	}

	hc := site.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s.HTTPClient = hc
	if _, _, err := s.fetchMetadata(context.Background(), id); err != nil {
		t.Fatalf("valid document: %v", err)
	}
	if _, _, err := s.fetchMetadata(context.Background(), site.URL+"/redirect.json"); err == nil {
		t.Fatal("redirect followed")
	}
	for name, b := range map[string]string{
		"client_id mismatch": strings.Replace(doc(""), "/client.json", "/other.json", 1),
		"secret":             doc(`,"client_secret":"x"`),
		"private_key_jwt":    doc(`,"token_endpoint_auth_method":"private_key_jwt"`),
		"bad redirect":       strings.Replace(doc(""), "https://app.example/cb", "http://app.example/cb", 1),
		"implicit":           doc(`,"response_types":["token"]`),
		"too big":            doc(`,"x":"` + strings.Repeat("a", maxMetadataBytes) + `"`),
		"not json":           "<html>",
		"array":              "[]",
	} {
		body = b
		if _, _, err := s.fetchMetadata(context.Background(), id); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for _, bad := range []string{"https://app.example", "https://app.example/", "http://app.example/c.json", "https://u@app.example/c.json", "https://app.example/a/../c.json", "https://app.example/c.json#x"} {
		if _, _, err := s.fetchMetadata(context.Background(), bad); err == nil {
			t.Errorf("client id %s accepted", bad)
		}
	}
}
