package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
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
