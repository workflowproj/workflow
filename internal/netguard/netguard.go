// Package netguard provides an HTTP client for calls to user-supplied URLs (outbound webhooks,
// workflow post-functions). It refuses to connect to loopback, private, link-local and other
// non-public addresses so a webhook cannot be aimed at the cluster or a cloud metadata service.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

var ErrBlocked = errors.New("destination address is not allowed")

var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// HostAllowed reports whether host is on the allowlist (exact names, or ".domain" suffixes).
func HostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		switch {
		case a == "":
		case strings.HasPrefix(a, "."):
			if host == a[1:] || strings.HasSuffix(host, a) {
				return true
			}
		case host == a:
			return true
		}
	}
	return false
}

// Allowed reports whether an address may be dialled.
func Allowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range blocked {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// Client returns an HTTP client that checks every resolved address at connect time (so DNS
// rebinding cannot bypass the check). allowPrivate disables the guard for air-gapped installs
// whose receivers live on private networks. allowedHosts names hosts that may resolve to private
// addresses (e.g. a self-hosted Mattermost): an exact host name, or ".example.internal" for a
// domain and its subdomains. Every other destination stays guarded.
func Client(timeout time.Duration, allowPrivate bool, allowedHosts ...string) *http.Client {
	open := &net.Dialer{Timeout: 10 * time.Second}
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("%w: %s", ErrBlocked, host)
			}
			if !Allowed(ip) {
				return fmt.Errorf("%w: %s", ErrBlocked, ip)
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy: nil, // never route user-supplied URLs through an internal proxy
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if host, _, err := net.SplitHostPort(addr); err == nil && HostAllowed(host, allowedHosts) {
				return open.DialContext(ctx, network, addr)
			}
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          20,
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}
