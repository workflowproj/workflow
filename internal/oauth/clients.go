package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/workflowproj/workflow/internal/db/dbq"
	"github.com/workflowproj/workflow/internal/secure"
)

const (
	maxRedirectURIs = 10
	// maxMetadataBytes caps how much of a client ID metadata document is read.
	maxMetadataBytes = 16 << 10
	minMetadataCache = 5 * time.Minute
	maxMetadataCache = 24 * time.Hour
	defMetadataCache = time.Hour
)

// ---------------------------------------------------------------------------
// Redirect URIs

var schemeRE = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// unsafeSchemes can run code, read local data or reach other protocols; never redirect to them.
var unsafeSchemes = []string{"javascript", "data", "vbscript", "file", "about", "blob", "filesystem", "view-source",
	"ftp", "ws", "wss", "mailto", "tel", "sms", "intent", "jar", "chrome", "res", "resource"}

// checkRedirectURI enforces what a client may register as a redirect URI:
//   - https URLs;
//   - http only on a loopback host (127.0.0.1, [::1] or localhost), any port, for assistants
//     running on the person's computer (RFC 8252 §7.3);
//   - a private-use scheme such as cursor://… for desktop apps that receive the code through the
//     operating system (RFC 8252 §7.1). PKCE stops another app that claims the scheme from
//     redeeming the code, and the consent page shows that the app runs on the computer.
//
// Fragments, user info, whitespace and schemes that execute content are refused.
func checkRedirectURI(raw string) error {
	if raw == "" || len(raw) > 2000 || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return errors.New("redirect URIs must be non-empty URLs without spaces")
	}
	if strings.Contains(raw, "#") {
		return fmt.Errorf("redirect URI %q must not contain a fragment", raw)
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("redirect URI %q is not an absolute URL", raw)
	}
	if u.User != nil {
		return fmt.Errorf("redirect URI %q must not contain user info", raw)
	}
	switch scheme := strings.ToLower(u.Scheme); scheme {
	case "https":
		if u.Hostname() == "" {
			return fmt.Errorf("redirect URI %q has no host", raw)
		}
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("redirect URI %q must use https (http is allowed only for 127.0.0.1, [::1] and localhost)", raw)
		}
	default:
		if !schemeRE.MatchString(scheme) || slices.Contains(unsafeSchemes, scheme) || (u.Opaque == "" && u.Host == "" && u.Path == "") {
			return fmt.Errorf("redirect URI scheme %q is not allowed", scheme)
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackRedirect reports an http redirect URI on the loopback interface.
func isLoopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.EqualFold(u.Scheme, "http") && isLoopbackHost(u.Hostname())
}

// isNativeRedirect reports a redirect that lands on the person's computer (loopback or a
// private-use scheme) rather than a website.
func isNativeRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (isLoopbackRedirect(raw) || !strings.EqualFold(u.Scheme, "https"))
}

// redirectMatches compares a requested redirect URI with the registered ones: exactly, except
// that loopback http URIs match on any port (RFC 8252 §7.3; Claude Code and other native
// clients bind an ephemeral port). localhost and 127.0.0.1 are not interchangeable.
func redirectMatches(registered []string, requested string) bool {
	if slices.Contains(registered, requested) {
		return true
	}
	if !isLoopbackRedirect(requested) || strings.Contains(requested, "#") {
		return false
	}
	req, err := url.Parse(requested)
	if err != nil || req.User != nil {
		return false
	}
	for _, reg := range registered {
		if !isLoopbackRedirect(reg) {
			continue
		}
		u, err := url.Parse(reg)
		if err != nil {
			continue
		}
		if strings.EqualFold(u.Scheme, req.Scheme) && strings.EqualFold(u.Hostname(), req.Hostname()) &&
			u.EscapedPath() == req.EscapedPath() && u.RawQuery == req.RawQuery {
			return true
		}
	}
	return false
}

// redirectHost is what the consent page shows for a redirect URI: a host, or the scheme of a
// desktop app.
func redirectHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "http") {
		return u.Hostname()
	}
	return u.Scheme + "://"
}

// httpsURL keeps an optional https link (client_uri, logo_uri) and drops anything else.
func httpsURL(raw string) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2000 {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil {
		return nil
	}
	return &raw
}

// cleanName trims a self-asserted client name to something safe to show.
func cleanName(name, fallback string) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80])
	}
	if name == "" {
		return fallback
	}
	return name
}

// ---------------------------------------------------------------------------
// Dynamic client registration (RFC 7591)

type registration struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
	ClientID                string   `json:"client_id,omitempty"`
	ClientSecret            string   `json:"client_secret,omitempty"`
}

// validate checks client metadata shared by registration and metadata documents.
func (m *registration) validate() (string, error) {
	if len(m.RedirectURIs) == 0 {
		return "invalid_redirect_uri", errors.New("redirect_uris is required")
	}
	if len(m.RedirectURIs) > maxRedirectURIs {
		return "invalid_redirect_uri", fmt.Errorf("at most %d redirect URIs", maxRedirectURIs)
	}
	for _, u := range m.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			return "invalid_redirect_uri", err
		}
	}
	for _, g := range m.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			return "invalid_client_metadata", fmt.Errorf("grant type %q is not supported (authorization_code and refresh_token are)", g)
		}
	}
	if len(m.GrantTypes) > 0 && !slices.Contains(m.GrantTypes, "authorization_code") {
		return "invalid_client_metadata", errors.New("grant_types must include authorization_code")
	}
	for _, t := range m.ResponseTypes {
		if t != "code" {
			return "invalid_client_metadata", fmt.Errorf("response type %q is not supported (code is)", t)
		}
	}
	return "", nil
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.OAuth.DynamicRegistration {
		writeError(w, http.StatusForbidden, "access_denied", "dynamic client registration is disabled on this server")
		return
	}
	if limited(w, r, s.registerLimit) {
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusBadRequest, "invalid_client_metadata", "send the client metadata as application/json")
		return
	}
	var m registration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&m); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_client_metadata", "the request body is not valid client metadata JSON")
		return
	}
	if code, err := m.validate(); err != nil {
		writeError(w, http.StatusBadRequest, code, err.Error())
		return
	}
	method := m.TokenEndpointAuthMethod
	switch method {
	case "":
		// RFC 7591 defaults to client_secret_basic, but MCP clients are public clients using
		// PKCE; the response states the method so clients know not to expect a secret.
		method = "none"
	case "none", "client_secret_post", "client_secret_basic":
	default:
		writeError(w, http.StatusBadRequest, "invalid_client_metadata", fmt.Sprintf("token_endpoint_auth_method %q is not supported (none, client_secret_post and client_secret_basic are)", method))
		return
	}
	id := secure.Token(clientIDPrefix, 18)
	var secret string
	var secretHash *string
	if method != "none" {
		secret = secure.Token(clientSecretPrefix, 32)
		h := secure.Hash(secret)
		secretHash = &h
	}
	ip := clientIP(r)
	name := cleanName(m.ClientName, "Unnamed app ("+redirectHost(m.RedirectURIs[0])+")")
	c, err := s.Q.CreateOAuthClient(r.Context(), dbq.CreateOAuthClientParams{
		ID: id, Name: name, ClientUri: httpsURL(m.ClientURI), LogoUri: httpsURL(m.LogoURI), RedirectUris: m.RedirectURIs,
		TokenAuthMethod: method, SecretHash: secretHash, SoftwareID: optional(m.SoftwareID, 200), SoftwareVersion: optional(m.SoftwareVersion, 100),
		CreatedIp: &ip,
	})
	if err != nil {
		s.Log.Error("oauth client registration failed", "error", err)
		writeError(w, http.StatusInternalServerError, "server_error", "registration failed")
		return
	}
	s.Log.Info("oauth client registered", "client_id", c.ID, "name", c.Name, "redirect_uris", c.RedirectUris)
	resp := map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectUris,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
		"scope":                      strings.Join(Scopes, " "),
	}
	if c.ClientUri != nil {
		resp["client_uri"] = *c.ClientUri
	}
	if c.LogoUri != nil {
		resp["logo_uri"] = *c.LogoUri
	}
	if secret != "" {
		resp["client_secret"] = secret
		resp["client_secret_expires_at"] = 0
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, resp)
}

func optional(s string, max int) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len(s) > max {
		s = s[:max]
	}
	return &s
}

// ---------------------------------------------------------------------------
// Client lookup and client ID metadata documents

var (
	errUnknownClient  = errors.New("unknown client")
	errClientDocument = errors.New("client metadata document unavailable")
)

// isMetadataClientID reports a client id that is a client ID metadata document URL.
func isMetadataClientID(id string) bool { return strings.HasPrefix(id, "https://") }

// checkMetadataURL validates a client identifier URL (CIMD §3): https, a path, no user info,
// fragment or dot segments.
func checkMetadataURL(raw string) error {
	if len(raw) > 2000 || strings.Contains(raw, "#") {
		return errors.New("invalid client id URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path == "" || u.Path == "/" {
		return errors.New("client id URLs must be https URLs with a path")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return errors.New("client id URLs must not contain dot segments")
		}
	}
	return nil
}

// client loads a client. A client ID metadata document is fetched when it is not cached or the
// cache has expired, if fetch is set (the authorisation endpoint); the token endpoint uses the
// copy cached when the person authorised the client.
func (s *Server) client(ctx context.Context, id string, r *http.Request, fetch bool) (dbq.OauthClient, error) {
	if id == "" || len(id) > 2000 {
		return dbq.OauthClient{}, errUnknownClient
	}
	if !isMetadataClientID(id) {
		c, err := s.Q.GetOAuthClient(ctx, id)
		if err != nil || c.Kind != "dynamic" {
			return dbq.OauthClient{}, errUnknownClient
		}
		return c, nil
	}
	if !s.Cfg.OAuth.ClientMetadataDocuments {
		return dbq.OauthClient{}, errUnknownClient
	}
	c, err := s.Q.GetOAuthClient(ctx, id)
	cached := err == nil && c.Kind == "metadata"
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return dbq.OauthClient{}, err
	}
	if cached && (!fetch || (c.RefreshAfter != nil && time.Now().Before(*c.RefreshAfter))) {
		return c, nil
	}
	if !fetch {
		return dbq.OauthClient{}, errUnknownClient
	}
	if r != nil {
		if ok, _ := s.fetchLimit.Allow(clientIP(r)); !ok {
			return dbq.OauthClient{}, fmt.Errorf("%w: too many requests", errClientDocument)
		}
	}
	doc, ttl, err := s.fetchMetadata(ctx, id)
	if err != nil {
		s.Log.Warn("client metadata document rejected", "client_id", id, "error", err)
		return dbq.OauthClient{}, fmt.Errorf("%w: %v", errClientDocument, err)
	}
	var ip *string
	if r != nil {
		v := clientIP(r)
		ip = &v
	}
	return s.Q.UpsertOAuthMetadataClient(ctx, dbq.UpsertOAuthMetadataClientParams{
		ID: id, Name: cleanName(doc.ClientName, metadataHost(id)), ClientUri: httpsURL(doc.ClientURI), LogoUri: httpsURL(doc.LogoURI),
		RedirectUris: doc.RedirectURIs, SoftwareID: optional(doc.SoftwareID, 200), SoftwareVersion: optional(doc.SoftwareVersion, 100),
		RefreshAfter: ptr(time.Now().Add(ttl)), CreatedIp: ip,
	})
}

// ClientHost is the host people see for a client: the host serving its metadata document
// (verified by fetching it), or the host its first redirect URI returns to.
func ClientHost(c dbq.OauthClient) string {
	if c.Kind == "metadata" {
		return metadataHost(c.ID)
	}
	if len(c.RedirectUris) > 0 {
		return redirectHost(c.RedirectUris[0])
	}
	return ""
}

func metadataHost(id string) string {
	if u, err := url.Parse(id); err == nil {
		return u.Hostname()
	}
	return id
}

// fetchMetadata retrieves and validates a client ID metadata document: no redirects, 200 only,
// a bounded size, client_id equal to the URL, valid redirect URIs, and public-client
// authentication (the document cannot carry a shared secret). Errors are never cached.
func (s *Server) fetchMetadata(ctx context.Context, id string) (*registration, time.Duration, error) {
	if err := checkMetadataURL(id); err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("fetching the document returned HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > maxMetadataBytes {
		return nil, 0, fmt.Errorf("the document is larger than %d bytes", maxMetadataBytes)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, 0, errors.New("the document is not a JSON object")
	}
	var doc registration
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, 0, fmt.Errorf("the document has invalid fields: %v", err)
	}
	if doc.ClientID != id {
		return nil, 0, fmt.Errorf("the document's client_id %q does not match its URL", doc.ClientID)
	}
	if _, has := raw["client_secret"]; has {
		return nil, 0, errors.New("the document must not contain a client_secret")
	}
	if m := doc.TokenEndpointAuthMethod; m != "" && m != "none" {
		return nil, 0, fmt.Errorf("token_endpoint_auth_method %q is not supported for client ID metadata documents (none is)", m)
	}
	if _, err := doc.validate(); err != nil {
		return nil, 0, err
	}
	return &doc, cacheLifetime(res.Header), nil
}

// cacheLifetime follows the response's Cache-Control max-age within [5 minutes, 24 hours].
func cacheLifetime(h http.Header) time.Duration {
	ttl := defMetadataCache
	for _, d := range strings.Split(h.Get("Cache-Control"), ",") {
		d = strings.TrimSpace(strings.ToLower(d))
		switch {
		case d == "no-store" || d == "no-cache":
			ttl = minMetadataCache
		case strings.HasPrefix(d, "max-age="):
			if n, err := strconv.Atoi(strings.TrimPrefix(d, "max-age=")); err == nil {
				ttl = time.Duration(n) * time.Second
			}
		}
	}
	return min(max(ttl, minMetadataCache), maxMetadataCache)
}

func ptr[T any](v T) *T { return &v }
