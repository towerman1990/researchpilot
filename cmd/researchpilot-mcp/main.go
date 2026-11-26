package main

import (
	"context"
	"errors"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"researchpilot/internal/knowledge"
)

type searchInput struct {
	Query string `json:"query" jsonschema:"Search question or technical terms"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum evidence passages from 1 to 8 (default 4)"`
}

type searchOutput struct {
	Version  string              `json:"version"`
	Mode     string              `json:"mode"`
	Passages []knowledge.Passage `json:"passages"`
}

type evidenceInput struct {
	ID string `json:"id" jsonschema:"Evidence ID returned by search_knowledge"`
}

type evidenceOutput struct {
	Version string            `json:"version"`
	Passage knowledge.Passage `json:"passage"`
}

func knowledgeServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "researchpilot-knowledge", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "search_knowledge", Description: "Search the ResearchPilot public demo knowledge base.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, searchOutput, error) {
			limit := input.Limit
			if limit == 0 {
				limit = 4
			}
			passages, mode, err := knowledge.SearchDemo(ctx, input.Query, limit)
			return nil, searchOutput{Version: knowledge.DemoVersion, Mode: mode, Passages: passages}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_evidence", Description: "Read a passage from the public demo index by its ID.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, input evidenceInput) (*mcp.CallToolResult, evidenceOutput, error) {
			index, err := knowledge.Demo(ctx, nil)
			if err != nil {
				return nil, evidenceOutput{}, err
			}
			passage, ok := index.Get(input.ID)
			if !ok {
				return nil, evidenceOutput{}, errors.New("knowledge_evidence_not_found")
			}
			return nil, evidenceOutput{Version: knowledge.DemoVersion, Passage: passage}, nil
		})
	return server
}

func main() {
	if err := knowledgeServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
