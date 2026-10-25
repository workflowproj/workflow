package identity

import (
	"strings"
	"testing"
)

func TestSafeReturnTo(t *testing.T) {
	cases := map[string]string{
		"/":                       "/",
		"/projects/PLAT/board":    "/projects/PLAT/board",
		"/issues?jql=a%20%3D%20b": "/issues?jql=a%20%3D%20b",
		"//evil.example":          "/",
		"/\\evil.example":         "/",
		"/\t/evil.example":        "/",
		"/\n/evil.example":        "/",
		"https://evil.example":    "/",
		"javascript:alert(1)":     "/",
		"projects":                "/",
		"":                        "/",
	}
	for in, want := range cases {
		if got := SafeReturnTo(in); got != want {
			t.Errorf("SafeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClaimGroups(t *testing.T) {
	claims := map[string]any{
		"groups":       []any{"/wfm-admins", "/wfm-admins", "/sales/wfm-team", "wfm-qa", 42, ""},
		"realm_access": map[string]any{"roles": []any{"wfm-ops", "offline_access"}},
		"single":       "wfm-solo",
	}
	got := claimGroups(claims, []string{"groups", "realm_access.roles", "single", "missing.path", "groups.nested"}, "wfm-")
	want := []string{"wfm-admins", "wfm-qa", "wfm-ops", "wfm-solo"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("claimGroups = %q, want %q", got, want)
	}
	if got := claimGroups(claims, []string{"groups"}, "/sales/"); strings.Join(got, ",") != "sales/wfm-team" {
		t.Errorf("prefix written as a path: %q", got)
	}
	if got := claimGroups(claims, []string{"groups"}, ""); len(got) != 3 {
		t.Errorf("no prefix keeps every string: %q", got)
	}
	if !looksLikeJWT("a.b.c") || looksLikeJWT("wfm_pat_abc") || looksLikeJWT("a..c") {
		t.Error("looksLikeJWT")
	}
}
