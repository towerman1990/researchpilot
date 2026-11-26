package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"researchpilot/internal/outbound"
)

var demoIndex struct {
	sync.Mutex
	index *Index
	retry time.Time
}

func SearchDemo(ctx context.Context, query string, limit int) ([]Passage, string, error) {
	index, err := configuredDemo(ctx)
	if err != nil {
		return nil, "", err
	}
	results, mode, err := index.Search(ctx, query, limit)
	if err == nil && mode == "keyword" && os.Getenv("KNOWLEDGE_EMBEDDING_URL") != "" {
		mode = "keyword_fallback"
	}
	return results, mode, err
}

func configuredDemo(ctx context.Context) (*Index, error) {
	demoIndex.Lock()
	defer demoIndex.Unlock()
	if demoIndex.index != nil && (demoIndex.retry.IsZero() || time.Now().Before(demoIndex.retry)) {
		return demoIndex.index, nil
	}
	if endpoint := os.Getenv("KNOWLEDGE_EMBEDDING_URL"); endpoint != "" {
		embed, err := configuredEmbed(endpoint)
		if err == nil {
			index, err := configuredIndex(ctx, embed)
			if err == nil {
				demoIndex.index, demoIndex.retry = index, time.Time{}
				return index, nil
			}
		}
		demoIndex.retry = time.Now().Add(time.Minute)
	}
	index, err := Demo(ctx, nil)
	demoIndex.index = index
	return index, err
}

func configuredIndex(ctx context.Context, embed Embed) (*Index, error) {
	if os.Getenv("KNOWLEDGE_MILVUS_COLLECTION") == "" {
		return Demo(ctx, embed)
	}
	index, err := Demo(ctx, nil)
	if err != nil {
		return nil, err
	}
	if err := index.configureRemote(); err != nil {
		return nil, err
	}
	index.embed = embed
	return index, nil
}

func configuredEmbed(endpoint string) (Embed, error) {
	address, err := url.Parse(endpoint)
	model := strings.TrimSpace(os.Getenv("KNOWLEDGE_EMBEDDING_MODEL"))
	if err != nil || address.User != nil || address.Fragment != "" || model == "" {
		return nil, errors.New("knowledge_invalid_embedding_config")
	}
	local := os.Getenv("KNOWLEDGE_ALLOW_LOOPBACK_EMBEDDING") == "true" &&
		address.Scheme == "http" && net.ParseIP(address.Hostname()).IsLoopback()
	var client *http.Client
	if local {
		client = &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	} else {
		if _, err := outbound.ValidateURL(endpoint); err != nil || address.Scheme != "https" {
			return nil, errors.New("knowledge_invalid_embedding_config")
		}
		client = outbound.Client(8*time.Second, false)
	}
	var token string
	if path := os.Getenv("KNOWLEDGE_EMBEDDING_KEY_FILE"); path != "" || !local {
		key, err := os.ReadFile(path)
		token = strings.TrimSpace(string(key))
		if err != nil || token == "" {
			return nil, errors.New("knowledge_invalid_embedding_config")
		}
	}
	return func(ctx context.Context, texts []string) ([][]float64, error) {
		payload, err := json.Marshal(map[string]any{"model": model, "input": texts})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, address.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("knowledge_embedding_unavailable")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("knowledge_embedding_unavailable")
		}
		var result struct {
			Data []struct {
				Index  *int      `json:"index"`
				Vector []float64 `json:"embedding"`
			} `json:"data"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result) != nil || len(result.Data) != len(texts) {
			return nil, errors.New("knowledge_invalid_embeddings")
		}
		vectors := make([][]float64, len(texts))
		for _, row := range result.Data {
			if row.Index == nil || *row.Index < 0 || *row.Index >= len(vectors) || vectors[*row.Index] != nil || len(row.Vector) == 0 {
				return nil, errors.New("knowledge_invalid_embeddings")
			}
			vectors[*row.Index] = row.Vector
		}
		return vectors, nil
	}, nil
}
