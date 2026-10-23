package identity

import "testing"

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
