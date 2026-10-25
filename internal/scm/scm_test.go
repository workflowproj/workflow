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
