package research

import (
	"context"
	"encoding/json"
	"fmt"
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
		!strings.Contains(sources[1].Excerpt, "example/dependency v1.2.3") ||
		!strings.Contains(sources[1].Excerpt, `Direct requirement "example/dependency" "v1.2.3"`) {
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

func TestGitHubManifestDeclarations(t *testing.T) {
	work := githubManifestExcerpt("group/go.work", []byte("go 1.25.0\nuse (\n ./api\n ../../outside\n)\n"), 10000)
	if !strings.Contains(work, `Go workspace member "group/api"`) || strings.Contains(work, `Go workspace member "../../outside"`) {
		t.Fatal(work)
	}
	module := githubManifestExcerpt("group/api/go.mod", []byte("module example/api\nreplace example/core => ../core\n"), 10000)
	if !strings.Contains(module, `Local replacement "example/core" -> "group/core"`) {
		t.Fatal(module)
	}
	pkg := githubManifestExcerpt("app/package.json", []byte(`{"name":"web","dependencies":{"core":"workspace:*","react":"^19.0.0"}}`), 10000)
	if !strings.Contains(pkg, `Workspace dependency "core" "workspace:*"`) ||
		!strings.Contains(pkg, `Declared dependency "react" "^19.0.0"`) {
		t.Fatal(pkg)
	}
}

func TestGitHubNPMLockUsesRootDirectRecords(t *testing.T) {
	raw := []byte(`{"lockfileVersion":3,"packages":{"":{"dependencies":{"react":"^19.0.0","core":"file:../core","missing":"^1"}},"node_modules/react":{"version":"19.2.0"},"node_modules/core":{"link":true,"version":"fake"},"node_modules/transitive":{"version":"4.2.0"}}}`)
	summary := githubDependencyExcerpt(t.Context(), "web/package-lock.json", raw, 10000)
	if !strings.Contains(summary, `"react" declared "^19.0.0"; locked version "19.2.0"`) ||
		!strings.Contains(summary, `"core" declared "file:../core"; linked package`) ||
		!strings.Contains(summary, `"missing" declared "^1"; root installation record unavailable`) ||
		strings.Contains(summary, "transitive") || strings.Contains(summary, "fake") {
		t.Fatal(summary)
	}
	if got := githubNPMLockExcerpt([]byte(`{"lockfileVersion":1}`), ""); !strings.Contains(got, "unavailable") {
		t.Fatal(got)
	}
}

func TestGitHubNPMLockTracesNestedAndHoistedDependencies(t *testing.T) {
	raw := []byte(`{"lockfileVersion":2,"packages":{
		"":{"dependencies":{"@demo/app":"^1","shared":"^3"}},
		"node_modules/@demo/app":{"version":"1.0.0","dependencies":{"shared":"^2","hoisted":"^4"},"optionalDependencies":{"absent":"^1"},"peerDependencies":{"peer":"^1"}},
		"node_modules/@demo/app/node_modules/shared":{"version":"2.1.0","dependencies":{"@demo/app":"^1","leaf":"^5"}},
		"node_modules/shared":{"version":"3.0.0"},
		"node_modules/hoisted":{"version":"4.0.0"},
		"node_modules/leaf":{"version":"5.0.0"},
		"node_modules/peer":{"version":"1.0.0"}
	}}`)
	summary := githubNPMLockExcerpt(raw, "")
	for _, expected := range []string{
		`"@demo/app"@"1.0.0" -> "shared"@"2.1.0" (lockfile path "node_modules/@demo/app/node_modules/shared")`,
		`"@demo/app"@"1.0.0" -> "hoisted"@"4.0.0"`,
		`"@demo/app"@"1.0.0" -> "shared"@"2.1.0" -> "leaf"@"5.0.0"`,
	} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("missing %s in %s", expected, summary)
		}
	}
	for _, unexpected := range []string{`-> "shared"@"3.0.0"`, `-> "@demo/app"`, `-> "peer"`, `-> "absent"`} {
		if strings.Contains(summary, unexpected) {
			t.Fatalf("unexpected %s in %s", unexpected, summary)
		}
	}
}

func TestGitHubNPMLockFollowsLocalLinksFromTheirTargets(t *testing.T) {
	raw := []byte(`{"lockfileVersion":3,"packages":{
		"":{"workspaces":["packages/*"]},
		"node_modules/@demo/app":{"link":true,"resolved":"packages/app"},
		"packages/app":{"version":"1.0.0","dependencies":{"@demo/app":"*","shared":"^2"}},
		"packages/app/node_modules/shared":{"version":"2.1.0","dependencies":{"leaf":"^5"}},
		"node_modules/shared":{"version":"3.0.0"},
		"node_modules/leaf":{"version":"5.0.0"},
		"node_modules/outside":{"link":true,"resolved":"../outside"},
		"../outside":{"version":"9.9.9"},
		"node_modules/loop":{"link":true,"resolved":"packages/loop"},
		"packages/loop":{"link":true,"resolved":"node_modules/loop"}
	}}`)
	summary := githubNPMLockExcerpt(raw, "")
	for _, expected := range []string{
		`"@demo/app" -> "packages/app"`,
		`"@demo/app"@"1.0.0" -> "shared"@"2.1.0" (lockfile path "packages/app/node_modules/shared")`,
		`"@demo/app"@"1.0.0" -> "shared"@"2.1.0" -> "leaf"@"5.0.0"`,
	} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("missing %s in %s", expected, summary)
		}
	}
	for _, unexpected := range []string{`"outside"`, `"loop"`, `-> "@demo/app"`, `"3.0.0"`} {
		if strings.Contains(summary, unexpected) {
			t.Fatalf("unexpected %s in %s", unexpected, summary)
		}
	}
}

func TestGitHubNPMLockPrioritizesRequestedPackageInLargeSummary(t *testing.T) {
	packages := map[string]any{
		"": map[string]any{"dependencies": map[string]string{"app": "^1"}},
	}
	children := map[string]string{"@demo/requested": "^2"}
	for i := range 50 {
		name := fmt.Sprintf("@demo/child-%02d", i)
		children[name] = "^1"
		packages["node_modules/"+name] = map[string]string{"version": "1.0.0"}
	}
	packages["node_modules/app"] = map[string]any{"version": "1.0.0", "dependencies": children}
	packages["node_modules/@demo/requested"] = map[string]string{"version": "2.0.0"}
	raw, err := json.Marshal(map[string]any{"lockfileVersion": 3, "packages": packages})
	if err != nil {
		t.Fatal(err)
	}
	summary := githubDependencyExcerpt(WithEvidenceGoal(t.Context(), "分析 @demo/requested 的引入链"), "package-lock.json", raw, 10000)
	path := `"app"@"1.0.0" -> "@demo/requested"@"2.0.0"`
	relevant, ordinary := strings.Index(summary, path), strings.Index(summary, `-> "@demo/child-00"`)
	if relevant < 0 || ordinary < 0 || relevant > ordinary {
		t.Fatalf("requested package should come first: %s", summary)
	}
}
