package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var collectionName = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_]{0,127}$")

type milvusClient struct {
	endpoint, token string
	http            *http.Client
}

func configuredMilvus() (*milvusClient, error) {
	endpoint, err := url.Parse(os.Getenv("KNOWLEDGE_MILVUS_URL"))
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || strings.Trim(endpoint.Path, "/") != "" {
		return nil, errors.New("knowledge_invalid_milvus_url")
	}
	var token string
	if file := os.Getenv("KNOWLEDGE_MILVUS_TOKEN_FILE"); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, errors.New("knowledge_invalid_milvus_token")
		}
		token = strings.TrimSpace(string(raw))
		if token == "" {
			return nil, errors.New("knowledge_invalid_milvus_token")
		}
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return &milvusClient{endpoint: strings.TrimRight(endpoint.String(), "/"), token: token, http: client}, nil
}

func (m *milvusClient) post(ctx context.Context, operation string, body map[string]any, data any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint+"/v2/vectordb/"+operation, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Type-Allow-Int64", "true")
	if m.token != "" {
		request.Header.Set("Authorization", "Bearer "+m.token)
	}
	response, err := m.http.Do(request)
	if err != nil {
		return fmt.Errorf("milvus %s: request failed", operation)
	}
	defer response.Body.Close()
	var envelope struct {
		Code *int
		Data json.RawMessage
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope) != nil || envelope.Code == nil {
		return fmt.Errorf("milvus %s: invalid response (HTTP %d)", operation, response.StatusCode)
	}
	if *envelope.Code != 0 {
		return fmt.Errorf("milvus %s: code %d", operation, *envelope.Code)
	}
	if data != nil && json.Unmarshal(envelope.Data, data) != nil {
		return fmt.Errorf("milvus %s: invalid payload", operation)
	}
	return nil
}

type vectorHit struct {
	ID    string   `json:"id"`
	Score *float64 `json:"distance"`
}

func (m *milvusClient) search(ctx context.Context, collection string, vector []float64, limit int, hnsw bool, scope string) ([]vectorHit, error) {
	params := map[string]any{}
	if hnsw {
		params["ef"] = max(64, limit)
	}
	var hits []vectorHit
	err := m.post(ctx, "entities/search", map[string]any{
		"collectionName": collection, "annsField": "vector", "data": [][]float64{vector},
		"searchParams": map[string]any{"metricType": "COSINE", "params": params},
		"filter":       fmt.Sprintf("scope == %q", scope), "limit": limit, "consistencyLevel": "Strong",
	}, &hits)
	return hits, err
}

func (m *milvusClient) indexedRows(ctx context.Context, name, kind string) (int, error) {
	var rows []struct {
		IndexType   string
		MetricType  string
		IndexedRows int
	}
	err := m.post(ctx, "indexes/describe", map[string]any{"collectionName": name, "indexName": "vector_index"}, &rows)
	if err != nil {
		return 0, err
	}
	if len(rows) != 1 || rows[0].IndexType != kind || rows[0].MetricType != "COSINE" {
		return 0, errors.New("knowledge_invalid_milvus_index")
	}
	return rows[0].IndexedRows, nil
}

func (m *milvusClient) prepare(ctx context.Context, name, kind, scope string, index *Index) error {
	base := map[string]any{"collectionName": name}
	var exists struct{ Has bool }
	if err := m.post(ctx, "collections/has", base, &exists); err != nil {
		return err
	}
	if exists.Has {
		var names []string
		if err := m.post(ctx, "indexes/list", base, &names); err != nil {
			return err
		}
		for _, indexName := range names {
			if indexName == "vector_index" {
				return m.load(ctx, base)
			}
		}
	} else {
		fields := []map[string]any{
			{"fieldName": "id", "dataType": "VarChar", "isPrimary": true, "elementTypeParams": map[string]string{"max_length": "128"}},
			{"fieldName": "scope", "dataType": "VarChar", "elementTypeParams": map[string]string{"max_length": "64"}},
			{"fieldName": "vector", "dataType": "FloatVector", "elementTypeParams": map[string]string{"dim": fmt.Sprint(len(index.vectors[0]))}},
		}
		if err := m.post(ctx, "collections/create", map[string]any{
			"collectionName": name, "schema": map[string]any{"autoId": false, "enableDynamicField": false, "fields": fields},
		}, nil); err != nil {
			return err
		}
	}
	batchSize := max(1, min(256, 100000/len(index.vectors[0])))
	for offset := 0; offset < len(index.passages); offset += batchSize {
		end := min(offset+batchSize, len(index.passages))
		rows := make([]map[string]any, 0, end-offset)
		for n := offset; n < end; n++ {
			rows = append(rows, map[string]any{"id": index.passages[n].ID, "scope": scope, "vector": index.vectors[n]})
		}
		if err := m.post(ctx, "entities/upsert", map[string]any{"collectionName": name, "data": rows}, nil); err != nil {
			return err
		}
	}
	if err := m.post(ctx, "collections/flush", base, nil); err != nil {
		return err
	}
	params := map[string]any{"index_type": kind}
	if kind == "HNSW" {
		params["M"], params["efConstruction"] = 16, 100
	}
	if err := m.post(ctx, "indexes/create", map[string]any{"collectionName": name, "indexParams": []map[string]any{
		{"fieldName": "vector", "indexName": "vector_index", "metricType": "COSINE", "params": params},
	}}, nil); err != nil {
		return err
	}
	return m.load(ctx, base)
}

func (m *milvusClient) load(ctx context.Context, base map[string]any) error {
	if err := m.post(ctx, "collections/load", base, nil); err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state struct{ LoadState string }
		if err := m.post(ready, "collections/get_load_state", base, &state); err != nil {
			return err
		}
		if state.LoadState == "LoadStateLoaded" {
			return nil
		}
		select {
		case <-ready.Done():
			return ready.Err()
		case <-ticker.C:
		}
	}
}
