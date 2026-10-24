package ratelimit

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// DefaultTrustedProxies are private and loopback ranges: in Kubernetes the ingress controller
// or load balancer forwarding requests sits in one of these.
var DefaultTrustedProxies = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "::1/128", "fc00::/7"}

// ClientIP is the address of the client that sent r. X-Forwarded-For is believed only as far as
// it was written by trusted proxies: walking it from the right, the first address that is not a
// trusted proxy is the client. A request that does not come from a trusted proxy is its own client,
// whatever headers it carries, so the header cannot be used to dodge per-address limits.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	peer := remoteAddr(r)
	if !isTrusted(peer, trusted) {
		return addrString(peer, r.RemoteAddr)
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		client = a.Unmap()
		if !isTrusted(client, trusted) {
			break
		}
	}
	return addrString(client, r.RemoteAddr)
}

// ParsePrefixes reads CIDRs (or bare addresses) for ClientIP.
func ParsePrefixes(list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, err
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, _ := netip.ParseAddr(host)
	return a.Unmap()
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	if !a.IsValid() {
		return false
	}
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func addrString(a netip.Addr, fallback string) string {
	if a.IsValid() {
		return a.String()
	}
	return fallback
}

type ipKey struct{}

// WithClientIP records the client address for the rest of the request.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

// ClientIPFrom returns the address recorded by WithClientIP, or "".
func ClientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(ipKey{}).(string)
	return ip
}
