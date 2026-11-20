package requestmeta

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestClientAddressIgnoresUntrustedForwarding(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	if ClientAddress(r.Context(), r.RemoteAddr, r.Header.Values("X-Forwarded-For")) != "192.0.2.1" {
		t.Fatal("untrusted forwarding accepted")
	}
}

func TestForwardedClientAddress(t *testing.T) {
	trusted := []net.IP{net.ParseIP("172.20.0.5")}
	for _, tc := range []struct{ peer, forwarded, want string }{
		{"172.20.0.5", "203.0.113.7", "203.0.113.7"},
		{"172.20.0.6", "203.0.113.7", "172.20.0.6"},
		{"172.20.0.5", "203.0.113.7, 192.0.2.4", "172.20.0.5"},
		{"172.20.0.5", "invalid", "172.20.0.5"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		if got := ForwardedClientAddress(tc.peer, trusted, r.Header.Values("X-Forwarded-For")); got != tc.want {
			t.Errorf("peer=%s forwarded=%s got=%s", tc.peer, tc.forwarded, got)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("X-Forwarded-For", "203.0.113.7")
	r.Header.Add("X-Forwarded-For", "192.0.2.4")
	if got := ForwardedClientAddress("172.20.0.5", trusted, r.Header.Values("X-Forwarded-For")); got != "172.20.0.5" {
		t.Fatal("accepted repeated header")
	}
}

func TestClientAddressResolvesConfiguredProxy(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_HOST", "localhost")
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := ClientAddress(r.Context(), r.RemoteAddr, r.Header.Values("X-Forwarded-For")); got != "203.0.113.7" {
		t.Fatalf("trusted proxy address = %s", got)
	}
	r.RemoteAddr = "192.0.2.3:1234"
	if got := ClientAddress(r.Context(), r.RemoteAddr, r.Header.Values("X-Forwarded-For")); got != "192.0.2.3" {
		t.Fatalf("untrusted address = %s", got)
	}
}
