package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const semanticThreshold = 0.35

func milvusScope(model string) string {
	sum := sha256.New()
	sum.Write(bytes.ReplaceAll(demo, []byte("\r\n"), []byte("\n")))
	sum.Write([]byte("\x00" + model))
	return fmt.Sprintf("%x", sum.Sum(nil))
}

func (i *Index) configureRemote() error {
	name := os.Getenv("KNOWLEDGE_MILVUS_COLLECTION")
	if name == "" {
		return nil
	}
	if !collectionName.MatchString(name) || strings.Contains(name, "_synthetic_") || (!strings.HasSuffix(name, "_flat") && !strings.HasSuffix(name, "_hnsw")) {
		return errors.New("knowledge_invalid_milvus_collection")
	}
	client, err := configuredMilvus()
	if err != nil {
		return err
	}
	scope := milvusScope(strings.TrimSpace(os.Getenv("KNOWLEDGE_EMBEDDING_MODEL")))
	positions := make(map[string]int, len(i.passages))
	for n, passage := range i.passages {
		positions[passage.ID] = n
	}
	i.remote = func(ctx context.Context, vector []float64) ([]float64, error) {
		hits, err := client.search(ctx, name, float32Values(vector), len(i.passages), strings.HasSuffix(name, "_hnsw"), scope)
		if err != nil {
			return nil, err
		}
		scores := make([]float64, len(i.passages))
		for _, hit := range hits {
			position, ok := positions[hit.ID]
			if !ok || hit.Score == nil {
				return nil, errors.New("knowledge_invalid_milvus_hit")
			}
			scores[position] = *hit.Score
		}
		return scores, nil
	}
	return nil
}

type MilvusComparison struct {
	Rows                int                 `json:"rows"`
	QueryEmbeddingP95MS float64             `json:"query_embedding_p95_ms"`
	Searches            []BackendComparison `json:"searches"`
}

type BackendComparison struct {
	Backend      string        `json:"backend"`
	Collection   string        `json:"collection,omitempty"`
	IndexedRows  *int          `json:"indexed_rows,omitempty"`
	SearchP95MS  float64       `json:"search_p95_ms"`
	RecallAt5    float64       `json:"recall_at_5"`
	AgreementAt5 float64       `json:"agreement_with_go_at_5"`
	Cases        []QueryResult `json:"cases"`
}

func compareMilvus(ctx context.Context, index *Index, model string) (*MilvusComparison, error) {
	rows := len(index.passages)
	if raw := os.Getenv("KNOWLEDGE_MILVUS_BENCH_ROWS"); raw != "" {
		var err error
		rows, err = strconv.Atoi(raw)
		if err != nil || rows < len(index.passages) || rows > 20000 || rows*len(index.vectors[0]) > 10000000 {
			return nil, errors.New("knowledge_invalid_milvus_bench_rows")
		}
	}
	data := *index
	data.passages = slices.Clone(index.passages)
	data.vectors = slices.Clone(index.vectors)
	dimension := len(index.vectors[0])
	rng := rand.New(rand.NewPCG(42, 2))
	for n := len(data.passages); n < rows; n++ {
		vector := make([]float64, dimension)
		for d := range vector {
			vector[d] = rng.NormFloat64()
		}
		data.vectors = append(data.vectors, vector)
		data.passages = append(data.passages, Passage{ID: fmt.Sprintf("synthetic-%05d", n)})
	}
	for n, vector := range data.vectors {
		data.vectors[n] = float32Values(vector)
	}
	// Include the exact stored vectors in the name so re-embedding a corpus
	// cannot silently reuse a collection built from different data.
	sum := sha256.New()
	scope := milvusScope(model)
	sum.Write([]byte(scope))
	if err := json.NewEncoder(sum).Encode(data.vectors); err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("rp_kb_%x", sum.Sum(nil)[:12])
	if rows != len(index.passages) {
		prefix += "_synthetic"
	}
	client, err := configuredMilvus()
	if err != nil {
		return nil, err
	}
	results := []BackendComparison{
		{Backend: "go_exact"},
		{Backend: "milvus_flat", Collection: prefix + "_flat"},
		{Backend: "milvus_hnsw", Collection: prefix + "_hnsw"},
	}
	for n, kind := range []string{"FLAT", "HNSW"} {
		if err := client.prepare(ctx, results[n+1].Collection, kind, scope, &data); err != nil {
			return nil, err
		}
		indexed, err := client.indexedRows(ctx, results[n+1].Collection, kind)
		if err != nil {
			return nil, err
		}
		results[n+1].IndexedRows = &indexed
	}
	embeddingTimes := make([]float64, 0, len(evaluationCases))
	var covered, comparisons int
	for _, test := range evaluationCases {
		start := time.Now()
		vectors, err := index.embed(ctx, []string{test.query})
		embeddingTimes = append(embeddingTimes, elapsedMS(start))
		if err != nil {
			return nil, err
		}
		if len(vectors) != 1 || !validVectors(vectors, dimension) {
			return nil, errors.New("knowledge_invalid_embeddings")
		}
		queryVector := float32Values(vectors[0])
		start = time.Now()
		scores := data.exactScores(queryVector)
		var exact []string
		for _, n := range rank(scores)[:min(5, rows)] {
			if scores[n] > semanticThreshold {
				exact = append(exact, data.passages[n].ID)
			}
		}
		exactTime := elapsedMS(start)
		if len(test.relevant) > 0 {
			covered++
		}
		if len(exact) > 0 {
			comparisons++
		}
		for n := range results {
			result := &results[n]
			ids, elapsed := exact, exactTime
			if n != 0 {
				start := time.Now()
				hits, err := client.search(ctx, result.Collection, queryVector, min(5, rows), n == 2, scope)
				elapsed = elapsedMS(start)
				if err != nil {
					return nil, err
				}
				ids = make([]string, 0, len(hits))
				for _, hit := range hits {
					if hit.Score == nil {
						return nil, errors.New("knowledge_invalid_milvus_hit")
					}
					if *hit.Score > semanticThreshold {
						ids = append(ids, hit.ID)
					}
				}
			}
			query := QueryResult{ID: test.id, Query: test.query, Relevant: test.relevant, Results: ids, Milliseconds: elapsed}
			result.Cases = append(result.Cases, query)
			if len(test.relevant) > 0 {
				result.RecallAt5 += recall(query, 5)
			}
			if len(exact) > 0 {
				result.AgreementAt5 += recall(QueryResult{Relevant: exact, Results: ids}, 5)
			}
		}
	}
	for n := range results {
		result := &results[n]
		result.RecallAt5 /= float64(covered)
		if comparisons > 0 {
			result.AgreementAt5 /= float64(comparisons)
		}
		times := make([]float64, 0, len(result.Cases))
		for _, query := range result.Cases {
			times = append(times, query.Milliseconds)
		}
		result.SearchP95MS = p95(times)
	}
	return &MilvusComparison{Rows: rows, QueryEmbeddingP95MS: p95(embeddingTimes), Searches: results}, nil
}

func float32Values(vector []float64) []float64 {
	var norm float64
	for _, value := range vector {
		norm += value * value
	}
	norm = math.Sqrt(norm)
	values := make([]float64, len(vector))
	for n, value := range vector {
		values[n] = float64(float32(value / norm))
	}
	return values
}

func elapsedMS(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

func p95(times []float64) float64 {
	slices.Sort(times)
	return times[int(math.Ceil(0.95*float64(len(times))))-1]
}
