package timetrack

import "testing"

func TestParse(t *testing.T) {
	cases := map[string]int64{
		"30m":                30 * 60,
		"2h":                 2 * 3600,
		"1h 30m":             90 * 60,
		"2h30m":              150 * 60,
		"1.5h":               90 * 60,
		"1,5h":               90 * 60,
		"90":                 90 * 60, // a bare number is minutes
		"0":                  0,
		"1d":                 8 * 3600,
		"1w":                 40 * 3600,
		"1w 2d 3h 30m":       (40+16+3)*3600 + 30*60,
		"2 days":             16 * 3600,
		"3 hours 15 minutes": 3*3600 + 15*60,
		"1 week":             40 * 3600,
		"  4H ":              4 * 3600,
		"1h, 1h":             2 * 3600,
		"30m 1h":             90 * 60,
		"0.5d":               4 * 3600,
		"1 hr 5 min":         65 * 60,
	}
	for in, want := range cases {
		got, err := Parse(in, Default)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %d, want %d", in, got, want)
		}
	}
	for _, bad := range []string{"", "  ", "abc", "2x", "h", "1h junk", "-2h", "2mo", "1.2.3h"} {
		if _, err := Parse(bad, Default); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestParseWithOtherUnits(t *testing.T) {
	u := Units{HoursPerDay: 7.5, DaysPerWeek: 4}
	if got, _ := Parse("1d", u); got != 27000 {
		t.Fatalf("7.5h day: %d", got)
	}
	if got, _ := Parse("1w", u); got != 4*27000 {
		t.Fatalf("4-day week: %d", got)
	}
}

func TestFormat(t *testing.T) {
	cases := map[int64]string{
		0:                        "0m",
		-60:                      "0m",
		29:                       "0m",
		30:                       "1m",
		60:                       "1m",
		90 * 60:                  "1h 30m",
		8 * 3600:                 "1d",
		9 * 3600:                 "1d 1h",
		40 * 3600:                "1w",
		(40+16+3)*3600 + 30*60:   "1w 2d 3h 30m",
		2 * 40 * 3600:            "2w",
		(40 * 3600) + 60:         "1w 1m",
		(80+8)*3600 + 45*60 + 10: "2w 1d 45m",
	}
	for in, want := range cases {
		if got := Format(in, Default); got != want {
			t.Errorf("Format(%d) = %q, want %q", in, got, want)
		}
	}
	u := Units{HoursPerDay: 7.5, DaysPerWeek: 5}
	if got := Format(27000+3600, u); got != "1d 1h" {
		t.Errorf("7.5h days: %q", got)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, s := range []string{"1w 2d 3h 30m", "45m", "3d", "1w 4h", "2w 1d 7h 59m"} {
		n, err := Parse(s, Default)
		if err != nil {
			t.Fatal(err)
		}
		if got := Format(n, Default); got != s {
			t.Errorf("round trip %q -> %d -> %q", s, n, got)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Default.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, u := range []Units{{0, 5}, {25, 5}, {8, 0}, {8, 8}, {-1, 5}} {
		if u.Validate() == nil {
			t.Errorf("%+v should be invalid", u)
		}
	}
}
