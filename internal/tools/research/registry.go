package research

import (
	"context"
	"net/http"
	"os"
	"time"

	"researchpilot/internal/repository/model"
	toolcontract "researchpilot/internal/tools"
)

type evidenceGoalKey struct{}

// WithEvidenceGoal supplies the research question used to select relevant passages.
func WithEvidenceGoal(ctx context.Context, goal string) context.Context {
	return context.WithValue(ctx, evidenceGoalKey{}, goal)
}

// NewRegistry registers the research tools with their argument schemas and timeouts.
func NewRegistry(client *http.Client) *toolcontract.Registry[[]model.Source] {
	registry := toolcontract.NewRegistry[[]model.Source]()
	for _, tool := range []struct {
		name    string
		timeout time.Duration
		handler func(context.Context, *http.Client, toolcontract.Input) ([]model.Source, error)
	}{
		{"fetch_page", 15 * time.Second, fetchPage},
		{"web_search", 45 * time.Second, webSearch},
		{"github_repo", 30 * time.Second, githubRepository},
	} {
		spec := toolcontract.Spec{Name: tool.name, Version: "1", Timeout: tool.timeout, MaxOutputBytes: 2 << 20, InputSchema: toolcontract.Schemas[tool.name]}
		if err := registry.Register(spec, func(ctx context.Context, input toolcontract.Input) ([]model.Source, error) {
			return tool.handler(ctx, client, input)
		}); err != nil {
			panic(err)
		}
	}
	if os.Getenv("MICROSOFT_LEARN_MCP_ENABLED") == "true" {
		const name = "microsoft_docs_search"
		spec := toolcontract.Spec{Name: name, Version: "1", Timeout: 25 * time.Second,
			MaxOutputBytes: maxLearnResponse, InputSchema: toolcontract.Schemas[name]}
		if err := registry.Register(spec, func(ctx context.Context, input toolcontract.Input) ([]model.Source, error) {
			return microsoftDocsSearch(ctx, client, input)
		}); err != nil {
			panic(err)
		}
	}
	return registry
}
