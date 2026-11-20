package handler

import (
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestTaskEndpointRejectsCrossOriginWrite(t *testing.T) {
	h := server.New()
	Tasks{Auth: Auth{Origin: "http://localhost:8080"}}.RegisterRoutes(h)
	result := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/research/tasks", nil, ut.Header{Key: "Origin", Value: "https://other.example"}, ut.Header{Key: "Content-Type", Value: "application/json"})
	if result.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", result.Code)
	}
}
