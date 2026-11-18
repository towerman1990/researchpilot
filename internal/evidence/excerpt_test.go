package evidence

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLongDocumentKeepsTargetSection(t *testing.T) {
	doc := strings.Repeat("navigation and unrelated overview. ", 200) + "ReadHeaderTimeout limits header parsing. IdleTimeout limits waiting for the next request." + strings.Repeat(" other material ", 200)
	selected := Select(doc, "Compare ReadHeaderTimeout and IdleTimeout", 2500)
	if !strings.Contains(selected, "ReadHeaderTimeout limits header parsing") || !strings.Contains(selected, "IdleTimeout") || len(selected) > 2500 {
		t.Fatal(selected)
	}
}
func TestExcerptRespectsUTF8AndByteBudget(t *testing.T) {
	out := Select(strings.Repeat("公开证据。", 1000), "公开证据", 503)
	if len(out) > 503 || !utf8.ValidString(out) {
		t.Fatal("invalid bounded excerpt")
	}
}
