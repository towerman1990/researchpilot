package knowledge

import (
	"context"
	"errors"
	"testing"
)

func TestDemoKeywordRanking(t *testing.T) {
	index, err := Demo(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	results, mode, err := index.Search(t.Context(), "HttpClient 怎样更新 DNS 解析缓存？", 1)
	if err != nil || mode != "keyword" || len(results) != 1 || results[0].ID != "httpclient-dns" {
		t.Fatalf("results=%+v mode=%s err=%v", results, mode, err)
	}
	results, _, err = index.Search(t.Context(), "量子纠缠", 4)
	if err != nil || len(results) != 0 {
		t.Fatalf("unrelated query returned evidence: %+v (%v)", results, err)
	}
}

func TestDemoHybridAndFallback(t *testing.T) {
	available := true
	embed := func(_ context.Context, texts []string) ([][]float64, error) {
		if len(texts) == 1 && !available {
			return nil, errors.New("fixture unavailable")
		}
		if len(texts) == 1 {
			return [][]float64{{1, 0}}, nil
		}
		return [][]float64{{0, 1}, {1, 0}, {0, 1}, {0, 1}, {0, 1}}, nil
	}
	index, err := Demo(t.Context(), embed)
	if err != nil {
		t.Fatal(err)
	}
	results, mode, err := index.Search(t.Context(), "域名变更", 1)
	if err != nil || mode != "hybrid" || len(results) != 1 || results[0].ID != "httpclient-dns" {
		t.Fatalf("results=%+v mode=%s err=%v", results, mode, err)
	}
	available = false
	results, mode, err = index.Search(t.Context(), "PooledConnectionLifetime DNS", 1)
	if err != nil || mode != "keyword_fallback" || len(results) != 1 || results[0].ID != "httpclient-dns" {
		t.Fatalf("keyword fallback failed: %+v %s %v", results, mode, err)
	}
}
