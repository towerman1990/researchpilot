package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskEndpointRejectsCrossOriginWrite(t *testing.T) {
	handler := (&apiServer{origin: "http://localhost:8080"}).routes()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/research/tasks", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://other.example")
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", result.Code)
	}
}

func TestSnapshotSequencePreservesPrecision(t *testing.T) {
	raw, err := json.Marshal(snapshot{Seq: 9007199254740993})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["event_seq"] != "9007199254740993" {
		t.Fatalf("event_seq = %#v", fields["event_seq"])
	}
}
