package config

import (
	"strings"
	"testing"
)

func TestOIDCProviderOptions(t *testing.T) {
	base := map[string]string{
		"WFM_OIDC_PROVIDERS": "kc", "WFM_OIDC_KC_ISSUER": "https://sso.example.com/realms/acme",
		"WFM_OIDC_KC_CLIENT_ID": "workflow", "WFM_OIDC_KC_CLIENT_SECRET": "s",
	}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	c, err := load(t, with(map[string]string{
		"WFM_OIDC_KC_GROUPS_CLAIM": "groups, realm_access.Roles", "WFM_OIDC_KC_GROUPS_PREFIX": "wfm-",
		"WFM_OIDC_KC_SINGLE_LOGOUT": "true", "WFM_OIDC_KC_API_AUDIENCE": "workflow-api",
		"WFM_OIDC_KC_API_CLIENTS": "ci-Pipeline=ci-bot, deploy = deployer",
	}))
	if err != nil {
		t.Fatal(err)
	}
	p := c.OIDCProviders[0]
	if strings.Join(p.GroupsClaim, "|") != "groups|realm_access.Roles" {
		t.Errorf("claim paths keep their case: %q", p.GroupsClaim)
	}
	if !p.SingleLogout || p.APIAudience != "workflow-api" || p.APIClients["ci-Pipeline"] != "ci-bot" || p.APIClients["deploy"] != "deployer" {
		t.Errorf("options: %+v", p)
	}

	for name, extra := range map[string]map[string]string{
		"api audience is the client id": {"WFM_OIDC_KC_API_AUDIENCE": "workflow"},
		"api clients need an audience":  {"WFM_OIDC_KC_API_CLIENTS": "ci=bot"},
		"malformed api clients":         {"WFM_OIDC_KC_API_AUDIENCE": "workflow-api", "WFM_OIDC_KC_API_CLIENTS": "ci"},
	} {
		if _, err := load(t, with(extra)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestKeycloakDirectory(t *testing.T) {
	kc := map[string]string{
		"WFM_OIDC_PROVIDERS": "keycloak", "WFM_OIDC_KEYCLOAK_ISSUER": "https://sso.example.com/realms/acme",
		"WFM_OIDC_KEYCLOAK_CLIENT_ID": "workflow", "WFM_OIDC_KEYCLOAK_CLIENT_SECRET": "s",
		"WFM_KEYCLOAK_ENABLED": "true", "WFM_KEYCLOAK_URL": "https://sso.example.com/", "WFM_KEYCLOAK_REALM": "acme",
		"WFM_KEYCLOAK_CLIENT_ID": "workflow-sync", "WFM_KEYCLOAK_CLIENT_SECRET": "s", "WFM_KEYCLOAK_SYNC_GROUP": "workflow-users",
	}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range kc {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	c, err := load(t, kc)
	if err != nil {
		t.Fatal(err)
	}
	k := c.Keycloak
	if k.URL != "https://sso.example.com" || k.Provider != "keycloak" || k.SyncGroup != "/workflow-users" || !k.SyncGroups || k.Interval.Minutes() != 15 {
		t.Errorf("keycloak: %+v", k)
	}
	for name, extra := range map[string]map[string]string{
		"two directories":    {"WFM_GOOGLE_DIRECTORY_ENABLED": "true", "WFM_GOOGLE_SERVICE_ACCOUNT_KEY": "{}", "WFM_GOOGLE_ADMIN_SUBJECT": "a@example.com"},
		"unknown provider":   {"WFM_KEYCLOAK_PROVIDER": "okta"},
		"missing realm":      {"WFM_KEYCLOAK_REALM": ""},
		"not a url":          {"WFM_KEYCLOAK_URL": "sso.example.com"},
		"negative interval":  {"WFM_KEYCLOAK_SYNC_INTERVAL": "-1m"},
		"missing the secret": {"WFM_KEYCLOAK_CLIENT_SECRET": ""},
	} {
		if _, err := load(t, with(extra)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := load(t, map[string]string{"WFM_KEYCLOAK_URL": "not checked while disabled"}); err != nil {
		t.Errorf("disabled Keycloak is not validated: %v", err)
	}
}
