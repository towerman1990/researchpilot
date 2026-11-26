package knowledge

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const DemoVersion = "public-tech-v1"

//go:embed demo.json
var demo []byte

type Passage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Excerpt string `json:"excerpt"`
	Terms   string `json:"terms,omitempty"`
}

type Embed func(context.Context, []string) ([][]float64, error)

type Index struct {
	passages []Passage
	vectors  [][]float64
	embed    Embed
	remote   func(context.Context, []float64) ([]float64, error)
}

func Demo(ctx context.Context, embed Embed) (*Index, error) {
	var passages []Passage
	if err := json.Unmarshal(demo, &passages); err != nil {
		return nil, err
	}
	if len(passages) == 0 {
		return nil, errors.New("knowledge_empty_corpus")
	}
	index := &Index{passages: passages, embed: embed}
	if embed != nil {
		texts := make([]string, len(passages))
		for n, passage := range passages {
			texts[n] = passage.Title + "\n" + passage.Excerpt
		}
		vectors, err := embed(ctx, texts)
		if err != nil {
			return nil, err
		}
		if len(vectors) != len(passages) || !validVectors(vectors, len(vectors[0])) {
			return nil, errors.New("knowledge_invalid_embeddings")
		}
		index.vectors = vectors
	}
	return index, nil
}

func (i *Index) Get(id string) (Passage, bool) {
	for _, passage := range i.passages {
		if passage.ID == id {
			passage.Terms = ""
			return passage, true
		}
	}
	return Passage{}, false
}

// Search returns candidates ranked by lexical overlap and, when configured,
// by semantic similarity. It returns lexical hits even when embedding is unavailable.
func (i *Index) Search(ctx context.Context, query string, limit int) ([]Passage, string, error) {
	mode := keywordMode
	if i.embed != nil {
		mode = hybridMode
	}
	return i.search(ctx, query, limit, mode, true)
}

type searchMode string

const (
	keywordMode searchMode = "keyword"
	vectorMode  searchMode = "vector"
	hybridMode  searchMode = "hybrid"
)

func (i *Index) search(ctx context.Context, query string, limit int, mode searchMode, allowFallback bool) ([]Passage, string, error) {
	if query = strings.TrimSpace(query); query == "" || !utf8.ValidString(query) || len(query) > 8192 || limit < 1 || limit > 8 {
		return nil, "", errors.New("knowledge_invalid_query")
	}
	scores := make([]float64, len(i.passages))
	if mode != vectorMode {
		fuse(scores, i.keywordScores(query), 0)
	}
	label := string(mode)
	if mode != keywordMode {
		semantic, err := i.semanticScores(ctx, query)
		if err != nil {
			if !allowFallback || ctx.Err() != nil {
				return nil, "", err
			}
			label = "keyword_fallback"
		} else {
			fuse(scores, semantic, semanticThreshold)
			if i.remote != nil {
				label = string(mode) + "_milvus"
			}
		}
	}
	results := make([]Passage, 0, limit)
	for _, n := range rank(scores) {
		if scores[n] <= 0 || len(results) == limit {
			break
		}
		passage := i.passages[n]
		passage.Terms = ""
		results = append(results, passage)
	}
	return results, label, nil
}

func (i *Index) keywordScores(query string) []float64 {
	lexical := make([]float64, len(i.passages))
	queryTokens := tokens(query)
	for n, p := range i.passages {
		for term := range tokens(p.Title + " " + p.Terms) {
			if queryTokens[term] {
				lexical[n] += 2
			}
		}
		for term := range tokens(p.Excerpt) {
			if queryTokens[term] {
				lexical[n]++
			}
		}
	}
	return lexical
}

func (i *Index) semanticScores(ctx context.Context, query string) ([]float64, error) {
	if i.embed == nil {
		return nil, errors.New("knowledge_embedding_unavailable")
	}
	vectors, err := i.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 || !validVectors(vectors, len(vectors[0])) {
		return nil, errors.New("knowledge_invalid_embeddings")
	}
	if i.remote != nil {
		return i.remote(ctx, vectors[0])
	}
	if len(vectors[0]) != len(i.vectors[0]) {
		return nil, errors.New("knowledge_invalid_embeddings")
	}
	return i.exactScores(vectors[0]), nil
}

func (i *Index) exactScores(query []float64) []float64 {
	semantic := make([]float64, len(i.passages))
	for n, vector := range i.vectors {
		var dot, a, b float64
		for k, value := range vector {
			dot += value * query[k]
			a += value * value
			b += query[k] * query[k]
		}
		semantic[n] = dot / math.Sqrt(a) / math.Sqrt(b)
	}
	return semantic
}

func tokens(text string) map[string]bool {
	set := map[string]bool{}
	var word strings.Builder
	var previous rune
	flush := func() {
		if word.Len() > 1 {
			set[word.String()] = true
		}
		word.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.Is(unicode.Han, r) {
			flush()
			if previous != 0 {
				set[string([]rune{previous, r})] = true
			}
			previous = r
			continue
		}
		previous = 0
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	for _, term := range []string{"the", "and", "for", "with", "from", "can", "how", "what", "does", "is", "in", "to", "of", "a", "an"} {
		delete(set, term)
	}
	return set
}

func rank(scores []float64) []int {
	order := make([]int, len(scores))
	for n := range scores {
		order[n] = n
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if scores[a] > scores[b] {
			return -1
		}
		if scores[a] < scores[b] {
			return 1
		}
		return 0
	})
	return order
}

func fuse(result, scores []float64, threshold float64) {
	for n, id := range rank(scores) {
		if scores[id] <= threshold {
			break
		}
		result[id] += 1 / float64(60+n+1)
	}
}

func validVectors(vectors [][]float64, size int) bool {
	if size < 1 || size > 8192 {
		return false
	}
	for _, vector := range vectors {
		if len(vector) != size {
			return false
		}
		var norm float64
		for _, value := range vector {
			norm += value * value
		}
		if norm <= 0 || math.IsInf(norm, 0) || math.IsNaN(norm) {
			return false
		}
	}
	return true
}
