// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	netmail "net/mail"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/ratelimit"
)

type Mode string

const (
	ModeWeb    Mode = "web"
	ModeHooks  Mode = "hooks"
	ModeWorker Mode = "worker"
	// ModeAll runs every mode in one process (development and single-node installs).
	ModeAll Mode = "all"
)

// DefaultDatabaseURL suits local development (`make db-up`).
const DefaultDatabaseURL = "postgres://wfm:wfm@localhost:5432/wfm?sslmode=disable"

// OIDCProvider is a generic OpenID Connect sign-in provider. Google is one instance.
type OIDCProvider struct {
	ID           string
	DisplayName  string
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// AllowedDomains restricts sign-in. For Google the `hd` claim is checked; otherwise the email domain.
	AllowedDomains []string
}

func (p OIDCProvider) IsGoogle() bool { return p.Issuer == GoogleIssuer }

const GoogleIssuer = "https://accounts.google.com"

type JITMode string

const (
	JITAuto     JITMode = "auto"
	JITEnabled  JITMode = "true"
	JITDisabled JITMode = "false"
)

type GoogleDirectory struct {
	Enabled bool
	// Service account key: JSON, base64-encoded JSON, or a file path.
	ServiceAccountKey string
	// Workspace admin impersonated through domain-wide delegation.
	AdminSubject string
	Customer     string
	// Limit provisioning to members of this group (email) and/or this org unit path.
	Group    string
	OrgUnit  string
	Interval time.Duration
	// Also sync Google groups (for group -> role mappings).
	SyncGroups bool
	// Only sync groups whose email starts with this prefix, e.g. "wfm-".
	GroupPrefix string
}

type S3 struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

func (s S3) Enabled() bool { return s.Endpoint != "" && s.Bucket != "" }

type Config struct {
	Mode        Mode
	HTTPAddr    string
	MetricsAddr string
	DatabaseURL string
	// PublicURL is where people reach the web UI and API.
	PublicURL string
	// HooksPublicURL is the internet-facing base for /hooks and /scim (defaults to PublicURL).
	HooksPublicURL string
	SecretKey      string
	SessionTTL     time.Duration
	LogLevel       string
	LogFormat      string
	DevLogin       bool
	Production     bool

	OIDCProviders   []OIDCProvider
	JITProvisioning JITMode
	BootstrapAdmins []string

	GoogleDirectory GoogleDirectory
	// Anonymise users deleted upstream after this long. 0 disables anonymisation.
	DeletedUserRetention time.Duration

	S3                 S3
	AttachmentMaxBytes int64

	WorkerConcurrency    int
	SCMReconcileInterval time.Duration
	// Maximum depth of automation rules triggering other rules.
	AutomationMaxDepth int
	// Allow outbound webhooks to private / loopback addresses (air-gapped installs).
	WebhookAllowPrivate bool
	// OutboundAllowedHosts may resolve to private addresses (e.g. a self-hosted Mattermost).
	OutboundAllowedHosts []string
	// Chat slash commands (/wfm): enabled per provider by its secret.
	SlackSigningSecret      string
	MattermostCommandTokens []string
	ChatCommandProjects     []string // project keys commands may read; empty = every open project

	// Email. Without a host, messages are kept in memory (and logged) instead of sent.
	SMTP SMTP

	RateLimit RateLimit
	// TrustedProxies may set X-Forwarded-For (client addresses for limits, sessions and audit).
	TrustedProxies []netip.Prefix
	// OAuth sign-in for AI assistants connecting to /mcp.
	OAuth OAuth
	// TimeTracking sets the working day and week that durations such as "1w 2d" use.
	TimeTracking TimeTracking
	// EmailBatchWindow delays immediate notification emails so that several changes to one
	// issue within the window reach a person as one email.
	EmailBatchWindow time.Duration
	// Local accounts: email and password sign-in for people an admin adds.
	Local LocalAccounts
}

// RateLimit is requests per minute (0 = no limit), each with a burst of half that.
type RateLimit struct {
	PerCredential int // per access token or browser session
	Anonymous     int // per client address, requests without credentials
	PerAddress    int // per client address, everything
	Hooks         int // per webhook endpoint (/hooks/...)
}

// OAuth configures Workflow as the OAuth authorisation server for MCP clients.
type OAuth struct {
	// Enabled lets assistants sign in through the browser; without it /mcp takes tokens only.
	Enabled bool
	// DynamicRegistration allows RFC 7591 client registration at /oauth/register.
	DynamicRegistration bool
	// ClientMetadataDocuments accepts https client ids pointing at client ID metadata documents.
	ClientMetadataDocuments bool
	AccessTokenTTL          time.Duration
	RefreshTokenTTL         time.Duration
}

type TimeTracking struct {
	HoursPerDay float64
	DaysPerWeek float64
}

type LocalAccounts struct {
	// Enabled shows the password form and accepts password sign-in (WFM_LOCAL_LOGIN).
	Enabled bool
	// Require2FA makes every local account set up two-factor authentication at sign-in.
	Require2FA bool
	// LockoutAttempts consecutive failures lock an account for LockoutDuration (0 disables).
	LockoutAttempts int
	LockoutDuration time.Duration
}

type SMTP struct {
	Host, Username, Password, From string
	Port                           int
	TLS                            string // starttls, tls or none
}

type env struct {
	get  func(string) string
	errs []error
}

func (e *env) str(key, def string) string {
	if v := strings.TrimSpace(e.get(key)); v != "" {
		return v
	}
	return def
}

func (e *env) boolean(key string, def bool) bool {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: expected a boolean, got %q", key, v))
		return def
	}
	return b
}

func (e *env) integer(key string, def int) int {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: expected an integer, got %q", key, v))
		return def
	}
	return n
}

func (e *env) float(key string, def float64) float64 {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: expected a number, got %q", key, v))
		return def
	}
	return f
}

func (e *env) duration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(e.get(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s: expected a duration such as 15m, got %q", key, v))
		return def
	}
	return d
}

func (e *env) list(key string) []string {
	var out []string
	for _, p := range strings.Split(e.get(key), ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads configuration from the process environment.
func Load() (*Config, error) { return LoadFrom(os.Getenv) }

// LoadFrom reads configuration using the given lookup function (handy in tests).
func LoadFrom(get func(string) string) (*Config, error) {
	e := &env{get: get}
	c := &Config{
		Mode:            Mode(e.str("WFM_MODE", string(ModeAll))),
		HTTPAddr:        e.str("WFM_HTTP_ADDR", ":8080"),
		MetricsAddr:     e.str("WFM_METRICS_ADDR", ":9090"),
		DatabaseURL:     e.str("WFM_DATABASE_URL", DefaultDatabaseURL),
		PublicURL:       strings.TrimRight(e.str("WFM_PUBLIC_URL", "http://localhost:8080"), "/"),
		SecretKey:       e.str("WFM_SECRET_KEY", ""),
		SessionTTL:      e.duration("WFM_SESSION_TTL", 7*24*time.Hour),
		LogLevel:        e.str("WFM_LOG_LEVEL", "info"),
		LogFormat:       e.str("WFM_LOG_FORMAT", "json"),
		DevLogin:        e.boolean("WFM_DEV_LOGIN", false),
		Production:      e.str("WFM_ENV", "production") == "production",
		JITProvisioning: JITMode(e.str("WFM_JIT_PROVISIONING", string(JITAuto))),
		BootstrapAdmins: e.list("WFM_BOOTSTRAP_ADMINS"),
		GoogleDirectory: GoogleDirectory{
			Enabled:           e.boolean("WFM_GOOGLE_DIRECTORY_ENABLED", false),
			ServiceAccountKey: e.str("WFM_GOOGLE_SERVICE_ACCOUNT_KEY", ""),
			AdminSubject:      e.str("WFM_GOOGLE_ADMIN_SUBJECT", ""),
			Customer:          e.str("WFM_GOOGLE_CUSTOMER", "my_customer"),
			Group:             strings.ToLower(e.str("WFM_GOOGLE_SYNC_GROUP", "")),
			OrgUnit:           e.str("WFM_GOOGLE_SYNC_ORG_UNIT", ""),
			Interval:          e.duration("WFM_GOOGLE_SYNC_INTERVAL", 15*time.Minute),
			SyncGroups:        e.boolean("WFM_GOOGLE_SYNC_GROUPS", true),
			GroupPrefix:       strings.ToLower(e.str("WFM_GOOGLE_GROUP_PREFIX", "")),
		},
		DeletedUserRetention: e.duration("WFM_DELETED_USER_RETENTION", 90*24*time.Hour),
		S3: S3{
			Endpoint:  e.str("WFM_S3_ENDPOINT", ""),
			Bucket:    e.str("WFM_S3_BUCKET", ""),
			Region:    e.str("WFM_S3_REGION", "us-east-1"),
			AccessKey: e.str("WFM_S3_ACCESS_KEY", ""),
			SecretKey: e.str("WFM_S3_SECRET_KEY", ""),
			UseSSL:    e.boolean("WFM_S3_USE_SSL", true),
		},
		AttachmentMaxBytes:      int64(e.integer("WFM_ATTACHMENT_MAX_MB", 25)) * 1024 * 1024,
		WorkerConcurrency:       e.integer("WFM_WORKER_CONCURRENCY", 10),
		SCMReconcileInterval:    e.duration("WFM_SCM_RECONCILE_INTERVAL", 30*time.Minute),
		AutomationMaxDepth:      e.integer("WFM_AUTOMATION_MAX_DEPTH", 5),
		OutboundAllowedHosts:    e.list("WFM_OUTBOUND_ALLOWED_HOSTS"),
		SlackSigningSecret:      e.str("WFM_SLACK_SIGNING_SECRET", ""),
		MattermostCommandTokens: e.list("WFM_MATTERMOST_COMMAND_TOKENS"),
		ChatCommandProjects:     e.list("WFM_CHAT_COMMAND_PROJECTS"),
		WebhookAllowPrivate:     e.boolean("WFM_WEBHOOK_ALLOW_PRIVATE", false),
		SMTP: SMTP{
			Host: e.str("WFM_SMTP_HOST", ""), Port: e.integer("WFM_SMTP_PORT", 587),
			Username: e.str("WFM_SMTP_USERNAME", ""), Password: e.str("WFM_SMTP_PASSWORD", ""),
			From: e.str("WFM_SMTP_FROM", "Workflow <workflow@localhost>"), TLS: e.str("WFM_SMTP_TLS", "starttls"),
		},
		EmailBatchWindow: e.duration("WFM_EMAIL_BATCH_WINDOW", 2*time.Minute),
		Local: LocalAccounts{
			Enabled: e.boolean("WFM_LOCAL_LOGIN", true), Require2FA: e.boolean("WFM_LOCAL_REQUIRE_2FA", false),
			LockoutAttempts: e.integer("WFM_LOCAL_LOCKOUT_ATTEMPTS", 10), LockoutDuration: e.duration("WFM_LOCAL_LOCKOUT_DURATION", 15*time.Minute),
		},
	}
	c.HooksPublicURL = strings.TrimRight(e.str("WFM_HOOKS_PUBLIC_URL", c.PublicURL), "/")
	// Limits are on by default in production only, so development and tests are not throttled.
	on := func(n int) int {
		if c.Production {
			return n
		}
		return 0
	}
	c.RateLimit = RateLimit{
		PerCredential: e.integer("WFM_RATE_LIMIT_PER_MINUTE", on(600)),
		Anonymous:     e.integer("WFM_RATE_LIMIT_ANONYMOUS_PER_MINUTE", on(120)),
		PerAddress:    e.integer("WFM_RATE_LIMIT_ADDRESS_PER_MINUTE", on(3000)),
		Hooks:         e.integer("WFM_RATE_LIMIT_HOOKS_PER_MINUTE", on(1200)),
	}
	proxies := e.list("WFM_TRUSTED_PROXIES")
	if len(proxies) == 0 {
		proxies = ratelimit.DefaultTrustedProxies
	}
	if p, err := ratelimit.ParsePrefixes(proxies); err != nil {
		e.errs = append(e.errs, fmt.Errorf("WFM_TRUSTED_PROXIES: %v", err))
	} else {
		c.TrustedProxies = p
	}
	c.OAuth = OAuth{
		Enabled:                 e.boolean("WFM_OAUTH_ENABLED", true),
		DynamicRegistration:     e.boolean("WFM_OAUTH_DYNAMIC_REGISTRATION", true),
		ClientMetadataDocuments: e.boolean("WFM_OAUTH_CLIENT_METADATA_DOCUMENTS", true),
		AccessTokenTTL:          e.duration("WFM_OAUTH_ACCESS_TOKEN_TTL", time.Hour),
		RefreshTokenTTL:         e.duration("WFM_OAUTH_REFRESH_TOKEN_TTL", 30*24*time.Hour),
	}
	if c.OAuth.AccessTokenTTL < time.Minute || c.OAuth.AccessTokenTTL > 24*time.Hour {
		e.errs = append(e.errs, errors.New("WFM_OAUTH_ACCESS_TOKEN_TTL must be between 1m and 24h"))
	}
	if c.OAuth.RefreshTokenTTL < time.Hour || c.OAuth.RefreshTokenTTL > 366*24*time.Hour {
		e.errs = append(e.errs, errors.New("WFM_OAUTH_REFRESH_TOKEN_TTL must be between 1h and 8784h"))
	}
	c.TimeTracking = TimeTracking{HoursPerDay: e.float("WFM_TIME_HOURS_PER_DAY", 8), DaysPerWeek: e.float("WFM_TIME_DAYS_PER_WEEK", 5)}
	if h := c.TimeTracking.HoursPerDay; !(h > 0 && h <= 24) {
		e.errs = append(e.errs, fmt.Errorf("WFM_TIME_HOURS_PER_DAY: must be more than 0 and at most 24, got %v", h))
	}
	if d := c.TimeTracking.DaysPerWeek; !(d > 0 && d <= 7) {
		e.errs = append(e.errs, fmt.Errorf("WFM_TIME_DAYS_PER_WEEK: must be more than 0 and at most 7, got %v", d))
	}
	c.OIDCProviders = loadOIDC(e)

	switch c.SMTP.TLS {
	case "starttls", "tls", "none":
	default:
		e.errs = append(e.errs, fmt.Errorf("WFM_SMTP_TLS: must be starttls, tls or none, got %q", c.SMTP.TLS))
	}
	if _, err := netmail.ParseAddress(c.SMTP.From); err != nil {
		e.errs = append(e.errs, fmt.Errorf("WFM_SMTP_FROM: %v", err))
	}
	if c.EmailBatchWindow < time.Second {
		e.errs = append(e.errs, fmt.Errorf("WFM_EMAIL_BATCH_WINDOW: must be at least 1s, got %s", c.EmailBatchWindow))
	}
	if c.Local.LockoutAttempts < 0 || c.Local.LockoutDuration <= 0 {
		e.errs = append(e.errs, errors.New("WFM_LOCAL_LOCKOUT_ATTEMPTS must be 0 or more and WFM_LOCAL_LOCKOUT_DURATION positive"))
	}
	switch c.Mode {
	case ModeWeb, ModeHooks, ModeWorker, ModeAll:
	default:
		e.errs = append(e.errs, fmt.Errorf("WFM_MODE: must be web, hooks, worker or all, got %q", c.Mode))
	}
	switch c.JITProvisioning {
	case JITAuto, JITEnabled, JITDisabled:
	default:
		e.errs = append(e.errs, fmt.Errorf("WFM_JIT_PROVISIONING: must be auto, true or false"))
	}
	if c.SecretKey == "" {
		if c.Production {
			e.errs = append(e.errs, errors.New("WFM_SECRET_KEY is required (32+ random characters)"))
		}
		c.SecretKey = "insecure-development-secret-key-change-me"
	} else if len(c.SecretKey) < 32 {
		e.errs = append(e.errs, errors.New("WFM_SECRET_KEY must be at least 32 characters"))
	}
	if c.DevLogin && c.Production {
		e.errs = append(e.errs, errors.New("WFM_DEV_LOGIN cannot be enabled when WFM_ENV=production"))
	}
	if c.GoogleDirectory.Enabled && (c.GoogleDirectory.ServiceAccountKey == "" || c.GoogleDirectory.AdminSubject == "") {
		e.errs = append(e.errs, errors.New("WFM_GOOGLE_DIRECTORY_ENABLED needs WFM_GOOGLE_SERVICE_ACCOUNT_KEY and WFM_GOOGLE_ADMIN_SUBJECT"))
	}
	if len(e.errs) > 0 {
		return nil, errors.Join(e.errs...)
	}
	return c, nil
}

// loadOIDC reads WFM_OIDC_PROVIDERS=google,okta and WFM_OIDC_<ID>_* for each provider.
// For convenience WFM_GOOGLE_CLIENT_ID/SECRET alone enables Google.
func loadOIDC(e *env) []OIDCProvider {
	ids := e.list("WFM_OIDC_PROVIDERS")
	if len(ids) == 0 && e.str("WFM_GOOGLE_CLIENT_ID", "") != "" {
		ids = []string{"google"}
	}
	globalDomains := e.list("WFM_ALLOWED_DOMAINS")
	var out []OIDCProvider
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		prefix := "WFM_OIDC_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_"
		p := OIDCProvider{
			ID:           id,
			DisplayName:  e.str(prefix+"DISPLAY_NAME", ""),
			Issuer:       strings.TrimRight(e.str(prefix+"ISSUER", ""), "/"),
			ClientID:     e.str(prefix+"CLIENT_ID", ""),
			ClientSecret: e.str(prefix+"CLIENT_SECRET", ""),
			Scopes:       []string{"openid", "email", "profile"},
		}
		if id == "google" {
			if p.Issuer == "" {
				p.Issuer = GoogleIssuer
			}
			if p.ClientID == "" {
				p.ClientID = e.str("WFM_GOOGLE_CLIENT_ID", "")
			}
			if p.ClientSecret == "" {
				p.ClientSecret = e.str("WFM_GOOGLE_CLIENT_SECRET", "")
			}
			if p.DisplayName == "" {
				p.DisplayName = "Google"
			}
		}
		if p.DisplayName == "" {
			p.DisplayName = strings.ToUpper(id[:1]) + id[1:]
		}
		if extra := e.list(prefix + "SCOPES"); len(extra) > 0 {
			p.Scopes = extra
		}
		p.AllowedDomains = e.list(prefix + "ALLOWED_DOMAINS")
		if len(p.AllowedDomains) == 0 {
			p.AllowedDomains = globalDomains
		}
		if p.Issuer == "" || p.ClientID == "" || p.ClientSecret == "" {
			e.errs = append(e.errs, fmt.Errorf("OIDC provider %q needs %sISSUER, %sCLIENT_ID and %sCLIENT_SECRET", id, prefix, prefix, prefix))
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID == "google" && out[j].ID != "google" })
	return out
}

// RunsWeb reports whether this process serves the UI and API.
func (c *Config) RunsWeb() bool { return c.Mode == ModeWeb || c.Mode == ModeAll }

// RunsHooks reports whether this process serves the internet-facing webhook and SCIM endpoints.
func (c *Config) RunsHooks() bool { return c.Mode == ModeHooks || c.Mode == ModeAll }

// RunsWorker reports whether this process executes background jobs.
func (c *Config) RunsWorker() bool { return c.Mode == ModeWorker || c.Mode == ModeAll }
