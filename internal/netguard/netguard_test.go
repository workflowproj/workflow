package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func TestAllowed(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.20.0.1", "192.168.1.1", "169.254.169.254", "::1", "fd00::1", "::ffff:127.0.0.1", "100.64.1.1"} {
		if Allowed(netip.MustParseAddr(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111"} {
		if !Allowed(netip.MustParseAddr(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestClientRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	_, err := Client(2*time.Second, false).Get(srv.URL)
	if err == nil || !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}
	res, err := Client(2*time.Second, true).Get(srv.URL)
	if err != nil || res.StatusCode != 204 {
		t.Fatalf("allowPrivate client: %v %v", res, err)
	}
}

func TestHostAllowed(t *testing.T) {
	allowed := []string{"mattermost.corp.example", ".internal.example"}
	for host, want := range map[string]bool{
		"mattermost.corp.example":  true,
		"MATTERMOST.corp.example.": true,
		"chat.internal.example":    true,
		"internal.example":         true,
		"evil-internal.example":    false,
		"corp.example":             false,
		"metadata.google.internal": false,
	} {
		if got := HostAllowed(host, allowed); got != want {
			t.Errorf("HostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestAllowedHostMayBePrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	if _, err := Client(time.Second, false).Get(srv.URL); !errors.Is(err, ErrBlocked) {
		t.Fatalf("loopback should be blocked, got %v", err)
	}
	res, err := Client(time.Second, false, u.Hostname()).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowlisted host: %v", err)
	}
	res.Body.Close()
}
