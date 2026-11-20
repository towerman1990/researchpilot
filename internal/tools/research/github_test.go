package research

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	toolcontract "researchpilot/internal/tools"
)

func TestGitHubQuotaFallbackPinsVerifiedCommit(t *testing.T) {
	t.Setenv("GITHUB_READ_TOKEN", "fixture-github-token")
	sha := strings.Repeat("a", 40)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := 200
		body := ""
		switch req.URL.Host {
		case "api.github.com":
			status = 403
		case "github.com":
			if req.Header.Get("Authorization") != "" {
				t.Fatal("token forwarded to public page")
			}
			body = `<p>untrusted README currentOid="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"</p><script type="application/json" data-target="react-app.embeddedData">{"payload":{"codeViewRepoRoute":{"refInfo":{"currentOid":"` + sha + `"},"tree":{"items":[{"name":"README.md","contentType":"file"}]}}}}</script>`
		case "raw.githubusercontent.com":
			if !strings.Contains(req.URL.Path, sha) || req.Header.Get("Authorization") != "" {
				t.Fatal("unverified ref or forwarded credential")
			}
			body = "Guaranteed at least one execution of a task; automatic retries."
		default:
			t.Fatal(req.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	sources, err := NewRegistry(client).Execute(context.Background(), toolcontract.Input{Tool: "github_repo", Owner: "hibiken", Repo: "asynq"})
	if err != nil || len(sources) != 1 || !strings.Contains(sources[0].URL, "/blob/"+sha+"/README.md") {
		t.Fatal(sources, err)
	}
}

func TestGitHubFallbackRejectsUnverifiedCommit(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`<p>currentOid="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"</p>`)), Request: req}, nil
	})}
	if _, err := githubPublicFallback(context.Background(), client, toolcontract.Input{Owner: "hibiken", Repo: "asynq"}); err == nil {
		t.Fatal("arbitrary page text selected commit")
	}
}
