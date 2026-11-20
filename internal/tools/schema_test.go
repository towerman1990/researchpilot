package tools

import "testing"

func TestInputValidate(t *testing.T) {
	for _, input := range []Input{
		{Tool: "fetch_page", URL: "https://go.dev/doc/"},
		{Tool: "github_repo", Owner: "golang", Repo: "go"},
		{Tool: "web_search", Query: "Go official"},
	} {
		if err := input.Validate(); err != nil {
			t.Errorf("valid input %+v: %v", input, err)
		}
	}
	for _, input := range []Input{
		{Tool: "shell", Query: "rm"},
		{Tool: "fetch_page", URL: "https://go.dev", Query: "extra"},
		{Tool: "github_repo", Owner: "..", Repo: "go"},
		{Tool: "web_search", Query: "  "},
	} {
		if err := input.Validate(); err == nil {
			t.Errorf("invalid input accepted: %+v", input)
		}
	}
}
