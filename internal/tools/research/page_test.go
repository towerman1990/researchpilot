package research

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	toolcontract "researchpilot/internal/tools"
)

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

func TestFetchPageFallsBackToMatchingOracleManual(t *testing.T) {
	const original = "https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html#skip-locked"
	const mirror = "https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/innodb-locking-reads.html#skip-locked"
	var requests []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.String())
		switch req.URL.String() {
		case original:
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("denied"))}, nil
		case mirror:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<main><p>irrelevant</p><h2 id="skip-locked">SKIP LOCKED skips locked rows.</h2></main>`))}, nil
		default:
			t.Fatalf("unexpected request: %s", req.URL)
			return nil, nil
		}
	})}
	sources, err := fetchPage(t.Context(), client, toolcontract.Input{URL: original})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(requests, []string{original, mirror}) || len(sources) != 1 || sources[0].URL != mirror ||
		!strings.Contains(sources[0].Excerpt, "SKIP LOCKED") || strings.Contains(sources[0].Excerpt, "irrelevant") {
		t.Fatalf("requests=%v sources=%+v", requests, sources)
	}
}

func TestFetchPageDoesNotMirrorUnrelatedFailures(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		status int
	}{
		{"different version", "https://dev.mysql.com/doc/refman/8.0/en/innodb-locking-reads.html", http.StatusForbidden},
		{"different host", "https://dev.mysql.com.example.org/doc/refman/8.4/en/innodb-locking-reads.html", http.StatusForbidden},
		{"unexpected query", "https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html?q=anything", http.StatusForbidden},
		{"not found", "https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
			})}
			if _, err := fetchPage(t.Context(), client, toolcontract.Input{URL: tt.url}); err == nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}
