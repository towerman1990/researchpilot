package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestDeepSeekLive(t *testing.T) {
	key := os.Getenv("LIVE_MODEL_KEY")
	if key == "" {
		t.Skip("LIVE_MODEL_KEY not provided")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := modelHTTPClient()
	defer client.CloseIdleConnections()
	config := modelConfig{BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", Key: key}
	content, usage, err := callModel(ctx, client, config, "Return JSON {\"ok\":true}.")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal([]byte(content), &response) != nil || !response.OK {
		t.Fatal("model did not return expected JSON")
	}
	t.Logf("DeepSeek JSON response verified; total tokens=%d", usage)
}
