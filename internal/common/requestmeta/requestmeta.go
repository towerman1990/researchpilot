package requestmeta

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

func ClientAddress(ctx context.Context, remote string, forwarded []string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return remote
	}
	proxyHost := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_HOST"))
	if proxyHost == "" {
		return host
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, proxyHost)
	if err != nil {
		return host
	}
	trusted := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		trusted = append(trusted, address.IP)
	}
	return ForwardedClientAddress(host, trusted, forwarded)
}

// Only the configured immediate proxy may supply a single client address.
// The proxy must replace untrusted forwarding headers rather than append them.
func ForwardedClientAddress(peer string, trusted []net.IP, values []string) string {
	peerIP := net.ParseIP(peer)
	if peerIP == nil {
		return peer
	}
	isTrusted := false
	for _, ip := range trusted {
		if ip.Equal(peerIP) {
			isTrusted = true
			break
		}
	}
	if !isTrusted {
		return peer
	}
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return peer
	}
	client := net.ParseIP(strings.TrimSpace(values[0]))
	if client == nil {
		return peer
	}
	return client.String()
}
