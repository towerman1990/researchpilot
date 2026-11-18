package main

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
	if _, err := resolvePublic(context.Background(), "rebind.example", lookup); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePublic(context.Background(), "rebind.example", lookup); err == nil {
		t.Fatal("rebind accepted")
	}
	mixed := func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("::1")}, nil
	}
	if _, err := resolvePublic(context.Background(), "mixed.example", mixed); err == nil {
		t.Fatal("mixed public/private DNS accepted")
	}
}
func TestRedirectPolicy(t *testing.T) {
	client := outboundClient(time.Second, true)
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
	noRedirect := outboundClient(time.Second, false)
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
	if _, err = readLimited(res, 1024); err == nil || err.Error() != "response_too_large" {
		t.Fatal("decompressed limit ignored", err)
	}
}

func TestDoHRequiresTrustedPublicIPBootstrap(t *testing.T) {
	for _, raw := range []string{"http://1.1.1.1/dns-query", "https://localhost/resolve", "https://dns.example/resolve", "https://127.0.0.1/resolve", "https://[::1]/resolve", "https://1.1.1.1:444/resolve", "https://user:password@1.1.1.1/resolve"} {
		if _, err := dohResolver(raw); err == nil {
			t.Fatal("unsafe DNS bootstrap", raw)
		}
	}
	if _, err := dohResolver("https://1.1.1.1/dns-query"); err != nil {
		t.Fatal(err)
	}
}

func TestToolCredentialsNeverFollowRedirects(t *testing.T) {
	for _, header := range []string{"Authorization", "X-Subscription-Token"} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example.com/collect"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		})}
		if _, err := requestText(context.Background(), client, "https://api.example.com/data", map[string]string{header: "fixture-secret"}); err == nil || calls != 1 {
			t.Fatal("credential redirect followed", header, calls, err)
		}
	}
}

func TestPageEvidencePrefersMainArticle(t *testing.T) {
	raw := `<nav>noise</nav><main><header>navigation</header><article><h1>Context</h1><p>Cancellation &amp; deadlines.</p><script>steal()</script></article></main><footer>noise</footer>`
	text := pageText(raw)
	if strings.Contains(text, "noise") || strings.Contains(text, "steal") || !strings.Contains(text, "Cancellation & deadlines.") {
		t.Fatal(text)
	}
}

func TestPageAnchorSkipsUnrelatedSections(t *testing.T) {
	raw := `<main><h2 id="Overview">irrelevant introduction</h2><h2 id="Server">ReadHeaderTimeout field definition.</h2><h2>More</h2></main>`
	text := pageTextAt(raw, "https://pkg.go.dev/net/http#Server")
	if strings.Contains(text, "irrelevant") || !strings.Contains(text, "ReadHeaderTimeout field definition") {
		t.Fatal(text)
	}
}

func TestUnquotedHTMLAnchor(t *testing.T) {
	raw := `<h2 id=intro>unrelated</h2><h3 id=last-event-id>Reconnection sends Last-Event-ID.</h3><p>after</p>`
	text := pageTextAt(raw, "https://html.spec.whatwg.org/example#last-event-id")
	if strings.Contains(text, "unrelated") || !strings.Contains(text, "Reconnection sends") {
		t.Fatal(text)
	}
}
