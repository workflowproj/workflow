package config

import (
	"strings"
	"testing"
	"time"
)

// keycloakVars is a valid Keycloak sign-in provider plus directory sync.
func keycloakVars(extra map[string]string) map[string]string {
	vars := map[string]string{
		"WFM_OIDC_PROVIDERS": "keycloak", "WFM_OIDC_KEYCLOAK_ISSUER": "https://sso.example.com/realms/acme",
		"WFM_OIDC_KEYCLOAK_CLIENT_ID": "workflow", "WFM_OIDC_KEYCLOAK_CLIENT_SECRET": "s",
		"WFM_KEYCLOAK_ENABLED": "true", "WFM_KEYCLOAK_URL": "https://sso.example.com/", "WFM_KEYCLOAK_REALM": "acme",
		"WFM_KEYCLOAK_CLIENT_ID": "workflow-sync", "WFM_KEYCLOAK_CLIENT_SECRET": "t",
	}
	for k, v := range extra {
		vars[k] = v
	}
	return vars
}

func TestKeycloakConfig(t *testing.T) {
	c, err := load(t, keycloakVars(map[string]string{
		"WFM_KEYCLOAK_SYNC_GROUP": "staff/workflow", "WFM_KEYCLOAK_GROUP_PREFIX": "WFM-", "WFM_KEYCLOAK_SYNC_INTERVAL": "1h",
	}))
	if err != nil {
		t.Fatal(err)
	}
	k := c.Keycloak
	if !k.Enabled || k.URL != "https://sso.example.com" || k.Realm != "acme" || k.Provider != "keycloak" || k.SyncGroup != "/staff/workflow" ||
		k.GroupPrefix != "WFM-" || !k.SyncGroups || k.Interval != time.Hour {
		t.Fatalf("keycloak = %+v", k)
	}

	c, err = load(t, keycloakVars(map[string]string{"WFM_KEYCLOAK_SYNC_GROUPS": "false"}))
	if err != nil || c.Keycloak.SyncGroups || c.Keycloak.SyncGroup != "" || c.Keycloak.Interval != 15*time.Minute {
		t.Fatalf("defaults: %+v, %v", c.Keycloak, err)
	}

	// Off by default, and then nothing else is required.
	if c, err = load(t, nil); err != nil || c.Keycloak.Enabled {
		t.Fatalf("default: %+v, %v", c.Keycloak, err)
	}
}

func TestKeycloakConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"both directories", keycloakVars(map[string]string{"WFM_GOOGLE_DIRECTORY_ENABLED": "true", "WFM_GOOGLE_SERVICE_ACCOUNT_KEY": "{}", "WFM_GOOGLE_ADMIN_SUBJECT": "a@example.com"}), "only one directory sync"},
		{"no url", keycloakVars(map[string]string{"WFM_KEYCLOAK_URL": ""}), "needs WFM_KEYCLOAK_URL"},
		{"no secret", keycloakVars(map[string]string{"WFM_KEYCLOAK_CLIENT_SECRET": ""}), "WFM_KEYCLOAK_CLIENT_SECRET"},
		{"bad url", keycloakVars(map[string]string{"WFM_KEYCLOAK_URL": "sso.example.com"}), "want an http(s) URL"},
		{"unknown provider", keycloakVars(map[string]string{"WFM_KEYCLOAK_PROVIDER": "sso"}), `"sso" is not one of WFM_OIDC_PROVIDERS`},
		{"no sign-in provider", map[string]string{"WFM_KEYCLOAK_ENABLED": "true", "WFM_KEYCLOAK_URL": "https://sso.example.com", "WFM_KEYCLOAK_REALM": "acme", "WFM_KEYCLOAK_CLIENT_ID": "x", "WFM_KEYCLOAK_CLIENT_SECRET": "y"}, "WFM_KEYCLOAK_PROVIDER"},
		{"negative interval", keycloakVars(map[string]string{"WFM_KEYCLOAK_SYNC_INTERVAL": "-1m"}), "WFM_KEYCLOAK_SYNC_INTERVAL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.vars)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
