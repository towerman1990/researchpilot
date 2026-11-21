package research

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"researchpilot/internal/common/textutil"
	evidencecontract "researchpilot/internal/evidence"
	"researchpilot/internal/repository/model"
	toolcontract "researchpilot/internal/tools"
)

var repoPart = regexp.MustCompile("^[A-Za-z0-9_.-]{1,100}$")

func githubRepository(ctx context.Context, client *http.Client, input toolcontract.Input) ([]model.Source, error) {
	if !repoPart.MatchString(input.Owner) || !repoPart.MatchString(input.Repo) || input.Owner == "." || input.Owner == ".." || input.Repo == "." || input.Repo == ".." {
		return nil, errors.New("invalid_repository")
	}
	base := "https://api.github.com/repos/" + url.PathEscape(input.Owner) + "/" + url.PathEscape(input.Repo)
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if token := os.Getenv("GITHUB_READ_TOKEN"); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	raw, err := requestText(ctx, client, base, headers)
	if err != nil {
		if input.Path == "" && (err.Error() == "upstream_http_403" || err.Error() == "upstream_http_429") {
			return githubPublicFallback(ctx, client, input)
		}
		return nil, err
	}
	var metadata struct {
		Private       bool
		Visibility    string
		DefaultBranch string `json:"default_branch"`
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.Private || (metadata.Visibility != "" && metadata.Visibility != "public") || metadata.DefaultBranch == "" {
		return nil, errors.New("repository_not_public")
	}
	commitRaw, err := requestText(ctx, client, base+"/commits/"+url.PathEscape(metadata.DefaultBranch), headers)
	if err != nil {
		return nil, err
	}
	var commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Tree struct{ SHA string }
		}
	}
	if json.Unmarshal(commitRaw, &commit) != nil || !commitID.MatchString(commit.SHA) {
		return nil, errors.New("invalid_commit")
	}
	repo := "https://github.com/" + input.Owner + "/" + input.Repo
	canonical := repo + "/tree/" + commit.SHA
	var sources []model.Source
	if input.Path == "" {
		headers["Accept"] = "application/vnd.github.raw+json"
		readme, readmeErr := requestTextLimit(ctx, client, base+"/readme?ref="+commit.SHA, headers, 256<<10)
		if readmeErr == nil {
			sources = append(sources, model.Source{Title: input.Owner + "/" + input.Repo + " README at " + commit.SHA, URL: canonical + "#readme", Excerpt: textutil.Clip(string(readme), 12000)})
		}
	}
	headers["Accept"] = "application/vnd.github+json"
	if commitID.MatchString(commit.Commit.Tree.SHA) {
		deep, treeErr := githubTreeSources(ctx, client, base, repo, commit.SHA, commit.Commit.Tree.SHA, input.Path, headers)
		if treeErr == nil {
			return append(sources, deep...), nil
		}
		if input.Path != "" {
			return nil, treeErr
		}
	}
	if input.Path != "" {
		return nil, errors.New("github_scoped_listing_unavailable")
	}
	directory, err := requestText(ctx, client, base+"/contents?ref="+commit.SHA, headers)
	if err != nil {
		if len(sources) == 0 {
			return nil, err
		}
		return sources, nil
	}
	var entries []struct {
		Name string
		Type string
		Size int
	}
	if json.Unmarshal(directory, &entries) != nil {
		if len(sources) == 0 {
			return nil, errors.New("invalid_github_directory")
		}
		return sources, nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Type+" "+entry.Name)
	}
	sources = append(sources, model.Source{Title: "Repository root at " + commit.SHA, URL: canonical, Excerpt: textutil.Clip(strings.Join(names, "\n"), 4000)})
	allowedFiles := map[string]bool{"go.mod": true, "package.json": true, "Cargo.toml": true, "pyproject.toml": true, "requirements.txt": true}
	filesRead := 0
	for _, entry := range entries {
		if !allowedFiles[entry.Name] || entry.Type != "file" || entry.Size > 256*1024 || filesRead >= 5 {
			continue
		}
		headers["Accept"] = "application/vnd.github.raw+json"
		file, err := requestTextLimit(ctx, client, base+"/contents/"+url.PathEscape(entry.Name)+"?ref="+commit.SHA, headers, 256<<10)
		if err != nil {
			continue
		}
		filesRead++
		sources = append(sources, model.Source{Title: entry.Name + " at " + commit.SHA, URL: "https://github.com/" + input.Owner + "/" + input.Repo + "/blob/" + commit.SHA + "/" + entry.Name, Excerpt: textutil.Clip(string(file), 12000)})
	}
	return sources, nil
}

var commitID = regexp.MustCompile("^[a-f0-9]{40}$")
var githubState = regexp.MustCompile(`(?s)<script[^>]*data-target="react-app.embeddedData"[^>]*>(.*?)</script>`)

// This path carries no GitHub token. A public page alone must reveal a verified
// commit from its structured route state; arbitrary README text cannot pick SHA.
func githubPublicFallback(ctx context.Context, client *http.Client, input toolcontract.Input) ([]model.Source, error) {
	base := "https://github.com/" + url.PathEscape(input.Owner) + "/" + url.PathEscape(input.Repo)
	raw, err := requestText(ctx, client, base, nil)
	if err != nil {
		return nil, errors.New("github_api_limited_and_public_page_unavailable")
	}
	match := githubState.FindSubmatch(raw)
	if len(match) != 2 {
		return nil, errors.New("github_commit_unavailable")
	}
	var state struct {
		Payload struct {
			Route struct {
				Ref struct {
					SHA string `json:"currentOid"`
				} `json:"refInfo"`
				Tree struct {
					Items []struct {
						Name string `json:"name"`
						Kind string `json:"contentType"`
					} `json:"items"`
				} `json:"tree"`
			} `json:"codeViewRepoRoute"`
		} `json:"payload"`
	}
	if json.Unmarshal(match[1], &state) != nil || !commitID.MatchString(state.Payload.Route.Ref.SHA) {
		return nil, errors.New("github_commit_unavailable")
	}
	sha := state.Payload.Route.Ref.SHA
	names := []string{}
	for _, item := range state.Payload.Route.Tree.Items {
		lower := strings.ToLower(item.Name)
		if item.Kind == "file" && (lower == "readme.md" || lower == "readme.rst" || lower == "readme") {
			names = append(names, item.Name)
		}
	}
	if len(names) == 0 {
		names = []string{"README.md", "README.rst", "README"}
	}
	for _, name := range names {
		readme, readErr := requestTextLimit(ctx, client, "https://raw.githubusercontent.com/"+url.PathEscape(input.Owner)+"/"+url.PathEscape(input.Repo)+"/"+sha+"/"+url.PathEscape(name), nil, 256<<10)
		if readErr != nil {
			continue
		}
		return []model.Source{{Title: input.Owner + "/" + input.Repo + " README at " + sha + " (public-page fallback; API metadata unavailable)", URL: base + "/blob/" + sha + "/" + url.PathEscape(name), Excerpt: textutil.Clip(string(readme), 16000)}}, nil
	}
	return nil, errors.New("github_readme_unavailable")
}

// githubFileURLs resolves only public, commit-pinned file links. Paths are
// escaped segment by segment and the raw request uses a fixed GitHub host.
func githubFileURLs(u *url.URL) (source, raw string) {
	if u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 5)
	if len(parts) != 5 || parts[2] != "blob" || !commitID.MatchString(parts[3]) ||
		!toolcontract.ValidGitHubPath(parts[4]) || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", ""
	}
	input := toolcontract.Input{Tool: "github_repo", Owner: parts[0], Repo: parts[1]}
	if input.Validate() != nil {
		return "", ""
	}
	repo := input.Owner + "/" + input.Repo
	suffix := parts[3] + "/" + escapeGitHubPath(parts[4])
	return "https://github.com/" + repo + "/blob/" + suffix, "https://raw.githubusercontent.com/" + repo + "/" + suffix
}

func fetchGitHubFile(ctx context.Context, client *http.Client, source, rawURL string) ([]model.Source, error) {
	fixedClient := *client
	fixedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	raw, err := requestTextLimit(ctx, &fixedClient, rawURL, nil, 256<<10)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return nil, errors.New("github_file_not_text")
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, errors.New("empty_page")
	}
	goal, _ := ctx.Value(evidenceGoalKey{}).(string)
	return []model.Source{{
		Title:   "Repository file " + source,
		URL:     source,
		Excerpt: evidencecontract.Select(text, goal, 18000),
	}}, nil
}
