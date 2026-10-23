// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeWeb    Mode = "web"
	ModeHooks  Mode = "hooks"
	ModeWorker Mode = "worker"
	// ModeAll runs every mode in one process (development and single-node installs).
	ModeAll Mode = "all"
)

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

	OIDCProviders  []OIDCProvider
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
		DatabaseURL:     e.str("WFM_DATABASE_URL", "postgres://wfm:wfm@localhost:5432/wfm?sslmode=disable"),
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
		AttachmentMaxBytes:   int64(e.integer("WFM_ATTACHMENT_MAX_MB", 25)) * 1024 * 1024,
		WorkerConcurrency:    e.integer("WFM_WORKER_CONCURRENCY", 10),
		SCMReconcileInterval: e.duration("WFM_SCM_RECONCILE_INTERVAL", 30*time.Minute),
		AutomationMaxDepth:   e.integer("WFM_AUTOMATION_MAX_DEPTH", 5),
		WebhookAllowPrivate:  e.boolean("WFM_WEBHOOK_ALLOW_PRIVATE", false),
	}
	c.HooksPublicURL = strings.TrimRight(e.str("WFM_HOOKS_PUBLIC_URL", c.PublicURL), "/")
	c.OIDCProviders = loadOIDC(e)

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
		e.errs = append(e.errs, errors.New("Google directory sync needs WFM_GOOGLE_SERVICE_ACCOUNT_KEY and WFM_GOOGLE_ADMIN_SUBJECT"))
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
