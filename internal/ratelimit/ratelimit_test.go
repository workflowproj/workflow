package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(60, 3) // one a second, bursts of three
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("burst %d refused", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait != time.Second {
		t.Fatalf("over the burst: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("keys are independent")
	}
	now = now.Add(1500 * time.Millisecond)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("refilled after a second")
	}
	l.Reset("a")
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatal("reset gives a full bucket")
		}
	}
	now = now.Add(time.Hour)
	l.Allow("c")
	if len(l.buckets) != 1 {
		t.Fatalf("idle full buckets are swept: %d left", len(l.buckets))
	}
	var nilLimiter *Limiter
	if ok, _ := nilLimiter.Allow("x"); !ok {
		t.Fatal("a nil limiter allows everything")
	}
}

func TestClientIP(t *testing.T) {
	trusted, err := ParsePrefixes(DefaultTrustedProxies)
	if err != nil {
		t.Fatal(err)
	}
	req := func(remote string, xff ...string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	cases := []struct {
		r    *http.Request
		want string
	}{
		{req("203.0.113.9:4000"), "203.0.113.9"},
		{req("203.0.113.9:4000", "1.2.3.4"), "203.0.113.9"},                     // not from a proxy: header ignored
		{req("10.0.0.5:80", "198.51.100.7"), "198.51.100.7"},                    // ingress in front
		{req("10.0.0.5:80", "6.6.6.6, 198.51.100.7, 10.0.0.9"), "198.51.100.7"}, // spoofed left part skipped
		{req("10.0.0.5:80", "6.6.6.6", "198.51.100.7"), "198.51.100.7"},         // several headers
		{req("127.0.0.1:5555"), "127.0.0.1"},
		{req("[::ffff:10.0.0.5]:80", "garbage"), "10.0.0.5"},
	}
	for i, c := range cases {
		if got := ClientIP(c.r, trusted); got != c.want {
			t.Errorf("case %d: %s, want %s", i, got, c.want)
		}
	}
}
