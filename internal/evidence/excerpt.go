package evidence

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var terms = regexp.MustCompile("[A-Za-z_][A-Za-z0-9_.-]{2,}")
var stop = map[string]bool{"https": true, "http": true, "www": true, "com": true, "org": true, "dev": true, "net": true, "docs": true, "latest": true, "html": true, "github.com": true, "pkg.go.dev": true, "raw.githubusercontent.com": true}

// Select preserves source order and surrounding text; it never generates claims.
// Gaps are marked explicitly. Byte limits apply after UTF-8 encoding.
func Select(text, query string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	keywords := map[string]int{}
	for _, term := range terms.FindAllString(query, -1) {
		lower := strings.ToLower(term)
		if stop[lower] {
			continue
		}
		weight := 1
		if term != lower {
			weight = 4
		}
		keywords[lower] = weight
	}
	runes := []rune(text)
	type block struct {
		index, score int
		text         string
	}
	var blocks []block
	for start := 0; start < len(runes); start += 480 {
		end := min(start+480, len(runes))
		s := string(runes[start:end])
		lower := strings.ToLower(s)
		score := 0
		for word, weight := range keywords {
			score += min(strings.Count(lower, word), 3) * weight
		}
		blocks = append(blocks, block{index: start, score: score, text: s})
	}
	ranked := append([]block(nil), blocks...)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	var chosen []block
	remaining := limit
	for _, b := range ranked {
		if remaining < 32 {
			break
		}
		cost := len(b.text) + 7
		if cost > remaining {
			if len(chosen) > 0 {
				continue
			}
			b.text = truncate(b.text, remaining-7)
			cost = len(b.text) + 7
		}
		chosen = append(chosen, b)
		remaining -= cost
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].index < chosen[j].index })
	var out strings.Builder
	previous := -480
	for _, b := range chosen {
		if b.index > previous+480 {
			out.WriteString("\n[...]\n")
		}
		out.WriteString(b.text)
		previous = b.index
	}
	return truncate(out.String(), limit)
}
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if limit <= 0 {
		return ""
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}
