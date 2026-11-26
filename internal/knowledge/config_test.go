package knowledge

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopbackEmbeddingRequiresOptIn(t *testing.T) {
	t.Setenv("KNOWLEDGE_EMBEDDING_MODEL", "loopback-fixture")
	t.Setenv("KNOWLEDGE_EMBEDDING_KEY_FILE", "")
	t.Setenv("KNOWLEDGE_ALLOW_LOOPBACK_EMBEDDING", "")
	if _, err := configuredEmbed("http://127.0.0.1:18124/v1/embeddings"); err == nil {
		t.Fatal("loopback endpoint accepted without opt-in")
	}
	t.Setenv("KNOWLEDGE_ALLOW_LOOPBACK_EMBEDDING", "true")
	for _, bad := range []string{"http://192.168.0.1/v1/embeddings", "http://localhost:18124/v1/embeddings", "http://example.com/v1/embeddings"} {
		if _, err := configuredEmbed(bad); err == nil {
			t.Fatalf("opt-in must accept only literal loopback HTTP endpoints: %s", bad)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Method != http.MethodPost {
			t.Error("unexpected auth or HTTP method")
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.2,0.8]}]}`))
	}))
	defer server.Close()
	embed, err := configuredEmbed(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := embed(t.Context(), []string{"测试"})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != 2 {
		t.Fatalf("invalid loopback result: %v, %v", vectors, err)
	}
}
