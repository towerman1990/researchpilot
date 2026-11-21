package research

import (
	"context"
	"io"
	"net/http"
	"net/url"
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

func TestGitHubScopedTreePinsNestedManifest(t *testing.T) {
	commit, root, packages, module := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	const api = "/repos/example/project"
	const scope = "packages/示例 模块"
	t.Setenv("GITHUB_READ_TOKEN", "")
	var readManifest bool
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case api:
			body = "{\"default_branch\":\"main\",\"visibility\":\"public\"}"
		case api + "/commits/main":
			body = "{\"sha\":\"" + commit + "\",\"commit\":{\"tree\":{\"sha\":\"" + root + "\"}}}"
		case api + "/git/trees/" + root:
			body = "{\"tree\":[{\"path\":\"packages\",\"type\":\"tree\",\"sha\":\"" + packages + "\"}]}"
		case api + "/git/trees/" + packages:
			body = "{\"tree\":[{\"path\":\"示例 模块\",\"type\":\"tree\",\"sha\":\"" + module + "\"}]}"
		case api + "/git/trees/" + module:
			if req.URL.Query().Get("recursive") != "1" {
				t.Fatal("expected recursive scope listing")
			}
			body = "{\"tree\":[{\"path\":\"src\",\"type\":\"tree\"},{\"path\":\"src/main.go\",\"type\":\"blob\",\"mode\":\"100644\"},{\"path\":\"service/go.mod\",\"type\":\"blob\",\"mode\":\"100644\",\"size\":90},{\"path\":\"link/package.json\",\"type\":\"blob\",\"mode\":\"120000\"},{\"path\":\"vendor/other/go.mod\",\"type\":\"blob\",\"mode\":\"100644\"}]}"
		case api + "/contents/" + scope + "/service/go.mod":
			if req.URL.Query().Get("ref") != commit {
				t.Fatal("dependency manifest is not pinned to the resolved commit")
			}
			readManifest = true
			body = "module example/service\nrequire example/dependency v1.2.3\n"
		default:
			t.Fatalf("unexpected read: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	sources, err := NewRegistry(client).Execute(t.Context(), toolcontract.Input{Tool: "github_repo", Owner: "example", Repo: "project", Path: scope})
	if err != nil || !readManifest || len(sources) != 2 {
		t.Fatalf("scoped sources: %v %v", sources, err)
	}
	if !strings.Contains(sources[0].URL, "/tree/"+commit+"/packages/") ||
		!strings.Contains(sources[0].Excerpt, "src/main.go") ||
		strings.Contains(sources[0].Excerpt, "vendor/") ||
		!strings.Contains(sources[1].URL, "/blob/"+commit+"/packages/") ||
		!strings.Contains(sources[1].Excerpt, "example/dependency v1.2.3") {
		t.Fatalf("missing or misleading repository evidence: %+v", sources)
	}
}

func TestGitHubTruncatedTreeUsesShallowDirectory(t *testing.T) {
	commit, root := strings.Repeat("a", 40), strings.Repeat("b", 40)
	const api = "/repos/example/project"
	t.Setenv("GITHUB_READ_TOKEN", "")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		status := 200
		switch req.URL.Path {
		case api:
			body = "{\"default_branch\":\"main\",\"visibility\":\"public\"}"
		case api + "/commits/main":
			body = "{\"sha\":\"" + commit + "\",\"commit\":{\"tree\":{\"sha\":\"" + root + "\"}}}"
		case api + "/readme":
			status = 404
		case api + "/git/trees/" + root:
			if req.URL.Query().Get("recursive") == "1" {
				body = "{\"truncated\":true,\"tree\":[{\"path\":\"partial/nested\",\"type\":\"tree\"}]}"
			} else {
				body = "{\"tree\":[{\"path\":\"cmd\",\"type\":\"tree\"}]}"
			}
		default:
			t.Fatalf("unexpected read: %s", req.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	sources, err := NewRegistry(client).Execute(t.Context(), toolcontract.Input{Tool: "github_repo", Owner: "example", Repo: "project"})
	if err != nil || len(sources) != 1 || !strings.Contains(sources[0].Excerpt, "Only the immediate directory") ||
		!strings.Contains(sources[0].Excerpt, "cmd/") || strings.Contains(sources[0].Excerpt, "partial/nested") {
		t.Fatalf("shallow fallback evidence: %v %v", sources, err)
	}
}

func TestGitHubRejectsUnsafeDirectory(t *testing.T) {
	for _, directory := range []string{"a/../b", "/etc", "a//b", "a\\b", "a/%2e%2e", "a\nb"} {
		if (toolcontract.Input{Tool: "github_repo", Owner: "example", Repo: "project", Path: directory}).Validate() == nil {
			t.Fatalf("invalid scoped directory accepted: %q", directory)
		}
	}
}

func TestFetchGitHubFileUsesPinnedRawText(t *testing.T) {
	t.Setenv("GITHUB_READ_TOKEN", "test-token")
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const source = "https://github.com/example/project/blob/" + sha + "/internal/retry%20loop.go"
	const content = "package retry\n\nfunc canRetry(used, limit int) bool { return used < limit }"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		want := "https://raw.githubusercontent.com/example/project/" + sha + "/internal/retry%20loop.go"
		if req.URL.String() != want || req.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected upstream request to %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(content))}, nil
	})}
	sources, err := fetchPage(t.Context(), client, toolcontract.Input{URL: source + "#L2"})
	if err != nil || len(sources) != 1 || sources[0].URL != source || sources[0].Excerpt != content {
		t.Fatalf("sources=%+v err=%v", sources, err)
	}
}

func TestGitHubFileURLsRejectAmbiguousLinks(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, target := range []string{
		"https://github.com/example/project/blob/main/retry.go",
		"https://github.com.example.net/example/project/blob/" + sha + "/retry.go",
		"https://github.com/example/project/blob/" + sha + "/dir%2fretry.go",
		"https://github.com/example/project/blob/" + sha + "/dir/%2e%2e/retry.go",
		"https://github.com/example/project/blob/" + sha + "/retry.go?x=y",
	} {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		if source, raw := githubFileURLs(u); source != "" || raw != "" {
			t.Fatalf("rewrote ambiguous URL %s", target)
		}
	}
}
