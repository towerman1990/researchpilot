package research

import (
	"strings"
	"testing"
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
