package scm

import (
	"strings"
	"testing"
)

func TestExtractKeys(t *testing.T) {
	allowed := map[string]bool{"PLAT": true, "OPS": true}
	cases := map[string]string{
		"PLAT-12":                             "PLAT-12",
		"feature/PLAT-12-checkout":            "PLAT-12",
		"feature/plat-12-checkout":            "PLAT-12",
		"PLAT-12_login_form":                  "PLAT-12",
		"bugfix/PLAT-7":                       "PLAT-7",
		"users/ada/plat-3/try-again":          "PLAT-3",
		"PLAT-1,PLAT-2 OPS-9":                 "PLAT-1 PLAT-2 OPS-9",
		"Fix PLAT-12: retry payments (OPS-4)": "PLAT-12 OPS-4",
		"PLAT-12 and PLAT-12 again":           "PLAT-12",
		"PLAT-12abc":                          "",
		"XPLAT-12":                            "",
		"PLAT-0":                              "",
		"OTHER-5":                             "",
		"release-2026":                        "",
	}
	for in, want := range cases {
		if got := strings.Join(ExtractKeys(allowed, in), " "); got != want {
			t.Errorf("ExtractKeys(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSmartCommits(t *testing.T) {
	allowed := map[string]bool{"PLAT": true}
	cmds := ParseSmartCommits(allowed, "PLAT-12 #start-work #comment Picked this up\nunrelated #done\nplat-13 #done")
	var got []string
	for _, c := range cmds {
		got = append(got, strings.Join(c.Keys, ",")+":"+c.Command+":"+c.Arg)
	}
	want := []string{"PLAT-12:start-work:", "PLAT-12:comment:Picked this up", "PLAT-13:done:"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCommitFooter(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	for url, want := range map[string]string{
		"https://github.com/acme/shop/commit/" + sha: "Via commit [01234567](https://github.com/acme/shop/commit/" + sha + ")",
		"":                       "Via commit `01234567`",
		"javascript:alert(1)":    "Via commit `01234567`",
		"https://x.test/a)b[c]d": "Via commit `01234567`",
	} {
		got := commitFooter(Commit{SHA: sha, URL: url})
		if got != want {
			t.Errorf("commitFooter(%q) = %q, want %q", url, got, want)
		}
		if strings.Contains(got, "<") {
			t.Errorf("commitFooter(%q) has HTML: %q", url, got)
		}
	}
}

func TestWebURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/shop/deployments": "https://github.com/acme/shop/deployments",
		"http://ci.internal/job/7":                 "http://ci.internal/job/7",
		"javascript:alert(1)":                      "",
		"data:text/html,hi":                        "",
		"https:///no-host":                         "",
		"":                                         "",
	} {
		if got := webURL(in); got != want {
			t.Errorf("webURL(%q) = %q, want %q", in, got, want)
		}
	}
}
