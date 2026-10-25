package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/workflowproj/workflow/internal/apperr"
	"github.com/workflowproj/workflow/internal/identity"
	"github.com/workflowproj/workflow/internal/ratelimit"
)

var rateLimited = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "wfm_rate_limited_total", Help: "Requests refused with 429, by which limit refused them.",
}, []string{"limit"})

// limits are the request limits of one process (see config.RateLimit).
type limits struct {
	credential, anonymous, address, hooks *ratelimit.Limiter
}

// limitState lives on the Server: web and hooks handlers of one process share it.
type limitState struct {
	once sync.Once
	set  *limits
}

func (s *Server) limits() *limits {
	s.limitState.once.Do(func() {
		mk := func(perMinute int) *ratelimit.Limiter {
			if perMinute <= 0 {
				return nil
			}
			return ratelimit.New(perMinute, max(perMinute/2, 10))
		}
		rl := s.Cfg.RateLimit
		s.limitState.set = &limits{credential: mk(rl.PerCredential), anonymous: mk(rl.Anonymous), address: mk(rl.PerAddress), hooks: mk(rl.Hooks)}
	})
	return s.limitState.set
}

// limited reports whether a path is subject to request limits: the API, sign-in, MCP and the
// internet-facing endpoints, but not the UI's static files or health checks.
func limited(path string) bool {
	for _, p := range []string{"/api/", "/auth/", "/mcp", "/hooks/", "/scim/", "/oauth/", "/.well-known/"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// clientAddress records the client's address (trusting X-Forwarded-For only from trusted
// proxies) and applies the request limits.
func (s *Server) clientAddress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ratelimit.ClientIP(r, s.Cfg.TrustedProxies)
		r = r.WithContext(ratelimit.WithClientIP(r.Context(), ip))
		if !limited(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		l := s.limits()
		check := func(name string, lim *ratelimit.Limiter, key string) bool {
			ok, wait := lim.Allow(key)
			if !ok {
				rateLimited.WithLabelValues(name).Inc()
				tooManyRequests(s, w, r, wait)
			}
			return ok
		}
		if !check("address", l.address, ip) {
			return
		}
		switch cred := credentialKey(r); {
		case strings.HasPrefix(r.URL.Path, "/hooks/"):
			// Webhook senders (GitHub, GitLab, chat) burst from shared addresses; limit per endpoint.
			if !check("hooks", l.hooks, r.URL.Path) {
				return
			}
		case cred != "":
			if !check("credential", l.credential, cred) {
				return
			}
		default:
			if !check("anonymous", l.anonymous, ip) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// credentialKey identifies the caller's token or session without a database lookup (an
// invalid credential still gets its own bucket, and the per-address limit still applies).
func credentialKey(r *http.Request) string {
	raw := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = "b:" + strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	} else if c, err := r.Cookie(identity.SessionCookie); err == nil && c.Value != "" {
		raw = "s:" + c.Value
	}
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:12])
}

func tooManyRequests(s *Server, w http.ResponseWriter, r *http.Request, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(max(secs, 1)))
	WriteProblem(s.Log, w, r, apperr.New(http.StatusTooManyRequests, "rate_limited", "too many requests; try again in %d seconds", max(secs, 1)))
}
