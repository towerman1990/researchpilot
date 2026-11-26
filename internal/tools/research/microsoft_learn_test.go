package research

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	toolcontract "researchpilot/internal/tools"
)

func TestMicrosoftLearnMCPFixedTopicAndSourceLinks(t *testing.T) {
	t.Setenv("MICROSOFT_LEARN_MCP_ENABLED", "true")
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != learnEndpoint || req.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected endpoint or credentials: %s", req.URL)
		}
		media, status, body := "application/json", http.StatusOK, ""
		headers := make(http.Header)
		if calls > 1 && (req.Header.Get("MCP-Protocol-Version") != learnProtocol || req.Header.Get("Mcp-Session-Id") != "fixture-session") {
			t.Fatal("negotiated protocol or session was discarded")
		}
		if req.Method == http.MethodDelete {
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: headers}, nil
		}
		var message struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&message); err != nil {
			t.Fatal(err)
		}
		switch message.Method {
		case "initialize":
			headers.Set("Mcp-Session-Id", "fixture-session")
			body = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`
		case "notifications/initialized":
			status = http.StatusAccepted
		case "tools/list":
			body = `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"microsoft_docs_search","inputSchema":{"type":"object","properties":{"query":{"type":"string"}}}}]}}`
		case "tools/call":
			if message.ID != 3 || message.Params.Name != "microsoft_docs_search" ||
				len(message.Params.Arguments) != 1 || message.Params.Arguments["query"] != "ASP.NET Core minimal API route handlers" {
				t.Fatalf("unexpected outgoing arguments: %+v", message.Params)
			}
			media = "text/event-stream"
			results := `{"results":[
				{"title":"Official fixture","contentUrl":"https://learn.microsoft.com/aspnet/core","content":"Official excerpt"},
				{"title":"Rejected","contentUrl":"https://other.example.com/","content":"Ignored"},
				{"title":"Duplicate","contentUrl":"https://learn.microsoft.com/aspnet/core","content":"Ignored"}
			]}`
			encoded, _ := json.Marshal(map[string]string{"type": "text", "text": results})
			body = "data: {\"jsonrpc\":\"2.0\",\"id\":9,\"result\":{}}\n\n" +
				fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":3,\ndata: \"result\":{\"content\":[%s]}}\n\n", encoded)
		default:
			t.Fatalf("unexpected method: %s", message.Method)
		}
		headers.Set("Content-Type", media)
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	tools := NewRegistry(client)
	if _, err := tools.Execute(t.Context(), toolcontract.Input{Tool: "microsoft_docs_search", Query: "arbitrary topic"}); err == nil || calls != 0 {
		t.Fatal("unrecognized topic reached the external transport")
	}
	sources, err := tools.Execute(t.Context(), toolcontract.Input{Tool: "microsoft_docs_search", Query: "aspnet-routing"})
	if err != nil || calls != 5 || len(sources) != 1 || sources[0].URL != "https://learn.microsoft.com/aspnet/core" || sources[0].Excerpt != "Official excerpt" {
		t.Fatalf("unexpected results: %+v requests=%d error=%v", sources, calls, err)
	}
}

func TestMicrosoftLearnMCPDisabledByDefault(t *testing.T) {
	t.Setenv("MICROSOFT_LEARN_MCP_ENABLED", "")
	if NewRegistry(nil).Has("microsoft_docs_search") {
		t.Fatal("Microsoft Learn was enabled without deployment configuration")
	}
}
