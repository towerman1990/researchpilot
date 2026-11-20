package outbound

import (
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolverRevalidatesEveryConnection(t *testing.T) {
	attempts := 0
	lookup := func(context.Context, string, string) ([]net.IP, error) {
		attempts++
		if attempts == 1 {
			return []net.IP{net.ParseIP("1.1.1.1")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	if _, err := ResolvePublic(context.Background(), "rebind.example", lookup); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePublic(context.Background(), "rebind.example", lookup); err == nil {
		t.Fatal("rebind accepted")
	}
	mixed := func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("::1")}, nil
	}
	if _, err := ResolvePublic(context.Background(), "mixed.example", mixed); err == nil {
		t.Fatal("mixed public/private DNS accepted")
	}
}

func TestRedirectPolicy(t *testing.T) {
	client := Client(time.Second, true)
	for _, raw := range []string{"http://169.254.169.254/latest", "http://[::ffff:127.0.0.1]/", "https://localhost/"} {
		req := httptest.NewRequest("GET", raw, nil)
		if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
			t.Fatal(raw)
		}
	}
	req := httptest.NewRequest("GET", "https://go.dev/", nil)
	if err := client.CheckRedirect(req, make([]*http.Request, 4)); err == nil {
		t.Fatal("redirect bound ignored")
	}
	noRedirect := Client(time.Second, false)
	if err := noRedirect.CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("model redirect accepted")
	}
}

func TestDecompressedBodyIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(w)
		io.WriteString(compressed, strings.Repeat("a", 8192))
		compressed.Close()
	}))
	defer server.Close()
	res, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReadLimited(res, 1024); err == nil || err.Error() != "response_too_large" {
		t.Fatal("decompressed limit ignored", err)
	}
}

func TestOutboundAddressPolicy(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.1.0.2", "169.254.169.254", "::1", "::ffff:127.0.0.1", "100.64.0.1", "198.18.0.1", "2002:7f00:1::"} {
		if PublicIP(net.ParseIP(value)) {
			t.Fatalf("unsafe IP allowed: %s", value)
		}
	}
	if !PublicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IPv4 denied")
	}
	for _, value := range []string{"file:///etc/passwd", "http://localhost", "http://u:p@example.com", "https://example.com:22"} {
		if _, err := ValidateURL(value); err == nil {
			t.Fatalf("unsafe URL allowed: %s", value)
		}
	}
}
