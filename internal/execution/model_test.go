package execution

import (
	"context"
	"encoding/json"
	"os"
	"researchpilot/internal/service"
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
	client := service.ModelHTTPClient()
	defer client.CloseIdleConnections()
	config := modelConfig{BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", Key: key}
	result, err := CallModel(ctx, client, config, "Return JSON {\"ok\":true}.")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal([]byte(result.Text), &response) != nil || !response.OK {
		t.Fatal("model did not return expected JSON")
	}
	t.Logf("DeepSeek JSON response verified; total tokens=%d", result.Tokens)
}
