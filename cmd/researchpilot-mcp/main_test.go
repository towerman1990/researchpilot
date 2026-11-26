package main

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestKnowledgeTools(t *testing.T) {
	t.Setenv("KNOWLEDGE_EMBEDDING_URL", "")
	ctx := t.Context()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := knowledgeServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "researchpilot-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 2 {
		t.Fatalf("unexpected tool list: %+v %v", list, err)
	}
	for _, call := range []*mcp.CallToolParams{
		{Name: "search_knowledge", Arguments: map[string]any{"query": "HttpClient DNS", "limit": 1}},
		{Name: "get_evidence", Arguments: map[string]any{"id": "httpclient-dns"}},
	} {
		result, err := session.CallTool(ctx, call)
		if err != nil || result.IsError || result.StructuredContent == nil {
			t.Fatalf("%s: %+v %v", call.Name, result, err)
		}
	}
}
