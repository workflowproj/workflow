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
