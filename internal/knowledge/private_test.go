package knowledge

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitDocumentBoundsLongUnicodeParagraphs(t *testing.T) {
	document := "第一段\r\n\r\n" + strings.Repeat("证", 901) + "\n\n结束"
	chunks := SplitDocument(document)
	if len(chunks) != 3 || chunks[0] != "第一段" || chunks[2] != "证\n\n结束" {
		t.Fatalf("unexpected paragraph boundaries: %d chunks", len(chunks))
	}
	for _, chunk := range chunks {
		if !utf8.ValidString(chunk) || utf8.RuneCountInString(chunk) > 900 {
			t.Fatal("chunk exceeds Unicode bound")
		}
	}
}
