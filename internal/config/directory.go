package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Keycloak syncs people and groups from a Keycloak realm through its Admin REST API (Keycloak's
// own SCIM support only receives users, so it cannot push them here). People sign in through
// the OIDC provider named by Provider; their Keycloak user id is the subject at that provider.
type Keycloak struct {
	Enabled bool
	// URL is Keycloak's base URL (https://sso.example.com), without /realms/... or /admin/...
	URL   string
	Realm string
	// ClientID and ClientSecret are a confidential client with service accounts enabled and the
	// realm-management roles view-users and query-groups.
	ClientID     string
	ClientSecret string
	// Provider is the id of the WFM_OIDC_PROVIDERS entry that signs people in against this realm.
	Provider string
	// SyncGroup limits provisioning to members of this group (a path such as /workflow-users).
	SyncGroup string
	// SyncGroups also syncs the realm's groups (for group -> role mappings), optionally only those
	// whose name starts with GroupPrefix.
	SyncGroups  bool
	GroupPrefix string
	Interval    time.Duration
}

func loadKeycloak(e *env) Keycloak {
	k := Keycloak{
		Enabled:      e.boolean("WFM_KEYCLOAK_ENABLED", false),
		URL:          strings.TrimRight(e.str("WFM_KEYCLOAK_URL", ""), "/"),
		Realm:        e.str("WFM_KEYCLOAK_REALM", ""),
		ClientID:     e.str("WFM_KEYCLOAK_CLIENT_ID", ""),
		ClientSecret: e.str("WFM_KEYCLOAK_CLIENT_SECRET", ""),
		Provider:     strings.ToLower(e.str("WFM_KEYCLOAK_PROVIDER", "keycloak")),
		SyncGroup:    e.str("WFM_KEYCLOAK_SYNC_GROUP", ""),
		SyncGroups:   e.boolean("WFM_KEYCLOAK_SYNC_GROUPS", true),
		GroupPrefix:  e.str("WFM_KEYCLOAK_GROUP_PREFIX", ""),
		Interval:     e.duration("WFM_KEYCLOAK_SYNC_INTERVAL", 15*time.Minute),
	}
	if k.SyncGroup != "" && !strings.HasPrefix(k.SyncGroup, "/") {
		k.SyncGroup = "/" + k.SyncGroup
	}
	return k
}

// validateDirectory checks the directory sync settings against each other and the sign-in
// providers: one directory at a time, and Keycloak people must be able to sign in.
func validateDirectory(c *Config) []error {
	var errs []error
	k := c.Keycloak
	if c.GoogleDirectory.Enabled && k.Enabled {
		errs = append(errs, fmt.Errorf("enable only one directory sync: WFM_GOOGLE_DIRECTORY_ENABLED or WFM_KEYCLOAK_ENABLED"))
	}
	if !k.Enabled {
		return errs
	}
	if k.URL == "" || k.Realm == "" || k.ClientID == "" || k.ClientSecret == "" {
		errs = append(errs, fmt.Errorf("WFM_KEYCLOAK_ENABLED needs WFM_KEYCLOAK_URL, WFM_KEYCLOAK_REALM, WFM_KEYCLOAK_CLIENT_ID and WFM_KEYCLOAK_CLIENT_SECRET"))
	}
	if u, err := url.Parse(k.URL); k.URL != "" && (err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "") {
		errs = append(errs, fmt.Errorf("WFM_KEYCLOAK_URL: want an http(s) URL such as https://sso.example.com, got %q", k.URL))
	}
	if !slices.ContainsFunc(c.OIDCProviders, func(p OIDCProvider) bool { return p.ID == k.Provider }) {
		errs = append(errs, fmt.Errorf("WFM_KEYCLOAK_PROVIDER: %q is not one of WFM_OIDC_PROVIDERS; people synced from Keycloak sign in through it", k.Provider))
	}
	if k.Interval < 0 {
		errs = append(errs, fmt.Errorf("WFM_KEYCLOAK_SYNC_INTERVAL: must not be negative"))
	}
	return errs
}

// rawList is a comma-separated list kept as written (list lower-cases), for case-sensitive
// values such as token claim names.
func (e *env) rawList(key string) []string {
	var out []string
	for _, p := range strings.Split(e.get(key), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pairs reads "a=x,b=y" into a map, keys and values kept as written.
func (e *env) pairs(key string) map[string]string {
	out := map[string]string{}
	for _, p := range e.rawList(key) {
		k, v, ok := strings.Cut(p, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			e.errs = append(e.errs, fmt.Errorf("%s: want key=value pairs separated by commas, got %q", key, p))
			continue
		}
		out[k] = v
	}
	return out
}
