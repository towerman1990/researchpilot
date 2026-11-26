package knowledge

import (
	"context"
	"strings"
)

// SplitDocument keeps paragraph boundaries when possible and limits each
// stored fragment to a bounded number of Unicode code points.
func SplitDocument(content string) []string {
	const limit = 900
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var chunks []string
	var current []rune
	for _, paragraph := range strings.Split(content, "\n\n") {
		text := []rune(strings.TrimSpace(paragraph))
		if len(text) == 0 {
			continue
		}
		if len(current) > 0 && len(current)+2+len(text) > limit {
			chunks = append(chunks, string(current))
			current = nil
		}
		for len(text) > limit {
			chunks = append(chunks, string(text[:limit]))
			text = text[limit:]
		}
		if len(current) > 0 {
			current = append(current, '\n', '\n')
		}
		current = append(current, text...)
	}
	if len(current) > 0 {
		chunks = append(chunks, string(current))
	}
	return chunks
}

func SearchPrivate(ctx context.Context, passages []Passage, query string, limit int) ([]Passage, error) {
	index := &Index{passages: passages}
	results, _, err := index.Search(ctx, query, limit)
	return results, err
}
