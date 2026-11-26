package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type milvusTransport func(*http.Request) (*http.Response, error)

func (f milvusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMilvusSearchContract(t *testing.T) {
	client := &milvusClient{endpoint: "http://example.invalid", http: &http.Client{
		Transport: milvusTransport(func(req *http.Request) (*http.Response, error) {
			var body struct {
				CollectionName string
				Filter         string
				SearchParams   struct {
					MetricType string
					Params     struct{ Ef int }
				}
			}
			if req.Method != "POST" || req.URL.Path != "/v2/vectordb/entities/search" || json.NewDecoder(req.Body).Decode(&body) != nil {
				t.Fatal("invalid search request")
			}
			if body.CollectionName != "demo_hnsw" || body.Filter != `scope == "public"` || body.SearchParams.MetricType != "COSINE" || body.SearchParams.Params.Ef < 5 {
				t.Fatalf("unexpected search body: %+v", body)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":[{"id":"httpclient-dns","distance":0.9}]}`))}, nil
		}),
	}}
	hits, err := client.search(t.Context(), "demo_hnsw", []float64{1}, 5, true, "public")
	if err != nil || len(hits) != 1 || hits[0].ID != "httpclient-dns" || hits[0].Score == nil || *hits[0].Score != 0.9 {
		t.Fatalf("unexpected hits: %+v (%v)", hits, err)
	}
}

func TestMilvusComparisonIntegration(t *testing.T) {
	endpoint := os.Getenv("TEST_MILVUS_URL")
	if endpoint == "" {
		t.Skip("TEST_MILVUS_URL not configured")
	}
	t.Setenv("KNOWLEDGE_MILVUS_URL", endpoint)
	t.Setenv("KNOWLEDGE_EMBEDDING_MODEL", "milvus-integration-fixture")
	embed := func(_ context.Context, texts []string) ([][]float64, error) {
		out := make([][]float64, len(texts))
		for n, text := range texts {
			raw := sha256.Sum256([]byte(text))
			for _, value := range raw {
				out[n] = append(out[n], float64(value)-128)
			}
		}
		return out, nil
	}
	index, err := Demo(t.Context(), embed)
	if err != nil {
		t.Fatal(err)
	}
	for _, rows := range []int{len(index.passages), 4096} {
		t.Setenv("KNOWLEDGE_MILVUS_BENCH_ROWS", fmt.Sprint(rows))
		result, err := compareMilvus(t.Context(), index, "milvus-integration-fixture")
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows != rows || len(result.Searches) != 3 {
			t.Fatalf("unexpected comparison: %+v", result)
		}
		for _, search := range result.Searches {
			indexed := "-"
			if search.IndexedRows != nil {
				indexed = fmt.Sprint(*search.IndexedRows)
			}
			t.Logf("rows=%d backend=%s indexed=%s agreement=%0.3f p95_ms=%0.3f", rows, search.Backend, indexed, search.AgreementAt5, search.SearchP95MS)
		}
		if result.Searches[1].AgreementAt5 < 0.999 {
			t.Fatalf("FLAT disagrees with exact scan: %.4f", result.Searches[1].AgreementAt5)
		}
		if rows == len(index.passages) {
			t.Setenv("KNOWLEDGE_MILVUS_COLLECTION", result.Searches[2].Collection)
			runtimeIndex, err := configuredIndex(t.Context(), embed)
			if err != nil || len(runtimeIndex.vectors) != 0 {
				t.Fatalf("Milvus runtime should use persisted passage vectors: %v", err)
			}
			passages, mode, err := runtimeIndex.Search(t.Context(), "HttpClient DNS", 4)
			if err != nil || mode != "hybrid_milvus" || len(passages) == 0 {
				t.Fatalf("runtime hybrid mode returned %+v %q (%v)", passages, mode, err)
			}
		}
	}
}
