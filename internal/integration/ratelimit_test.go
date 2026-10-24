package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestRequestLimits(t *testing.T) {
	e := newEnv(t,
		withEnv("WFM_RATE_LIMIT_PER_MINUTE", "20"),          // bursts of 10 per session or token
		withEnv("WFM_RATE_LIMIT_ANONYMOUS_PER_MINUTE", "20"), // bursts of 10 per address
		withEnv("WFM_RATE_LIMIT_ADDRESS_PER_MINUTE", "1000"),
		// The test server is on loopback: don't trust it as a proxy, so X-Forwarded-For is ignored.
		withEnv("WFM_TRUSTED_PROXIES", "10.0.0.0/8"),
	)
	ada := e.login("ada@example.com", "Ada") // one anonymous request
	bob := e.login("bob@example.com", "Bob") // and another

	// Each session has its own bucket.
	status := func(c *Client, n int, hdr ...string) []int {
		var out []int
		for i := 0; i < n; i++ {
			out = append(out, c.do("GET", "/api/v1/me", nil, hdr...).Status)
		}
		return out
	}
	got := status(ada, 11)
	eq(t, fmt.Sprint(got[:10]), fmt.Sprint([]int{200, 200, 200, 200, 200, 200, 200, 200, 200, 200}), "within the burst")
	eq(t, got[10], http.StatusTooManyRequests, "over the burst")
	eq(t, status(bob, 1)[0], 200, "other people are unaffected")

	r := ada.do("GET", "/api/v1/me", nil)
	eq(t, r.Status, http.StatusTooManyRequests, "still limited")
	if r.Header.Get("Retry-After") == "" || !strings.Contains(string(r.Body), `"rate_limited"`) {
		t.Fatalf("429 carries Retry-After and a problem: %v %s", r.Header, r.Body)
	}

	// Anonymous requests are limited per address, and a forged X-Forwarded-For doesn't help.
	anon := e.anon()
	var codes []int
	for i := 0; i < 10; i++ {
		codes = append(codes, anon.do("GET", "/api/v1/auth/providers", nil, "X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i)).Status)
	}
	if codes[len(codes)-1] != http.StatusTooManyRequests {
		t.Fatalf("anonymous burst (2 sign-ins already spent): %v", codes)
	}

	// Health checks and the UI are never limited.
	eq(t, anon.do("GET", "/healthz", nil).Status, 200, "health")
}
