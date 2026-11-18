package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

type evidence struct {
	ID      string `json:"source_id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Excerpt string `json:"excerpt"`
}
type toolInput struct {
	Tool  string `json:"tool"`
	Query string `json:"query,omitempty"`
	URL   string `json:"url,omitempty"`
	Owner string `json:"owner,omitempty"`
	Repo  string `json:"repo,omitempty"`
}

var stripScripts = regexp.MustCompile("(?is)<(script|style)[^>]*>.*?</(script|style)>")
var stripTags = regexp.MustCompile("(?s)<[^>]+>")
var spaceRuns = regexp.MustCompile("\\s+")
var repoPart = regexp.MustCompile("^[A-Za-z0-9_.-]{1,100}$")

func clip(value string, count int) string {
	if utf8.RuneCountInString(value) <= count {
		return value
	}
	return string([]rune(value)[:count])
}

func requestText(ctx context.Context, client *http.Client, raw string, headers map[string]string) ([]byte, error) {
	if _, err := validateURL(raw); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, errors.New("invalid_url")
	}
	request.Header.Set("User-Agent", "ResearchPilot/0.2")
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("fetch_failed")
	}
	return readLimited(response, 2<<20)
}

func collectEvidence(ctx context.Context, client *http.Client, input toolInput) ([]evidence, error) {
	switch input.Tool {
	case "fetch_page":
		raw, err := requestText(ctx, client, input.URL, nil)
		if err != nil {
			return nil, err
		}
		text := spaceRuns.ReplaceAllString(html.UnescapeString(stripTags.ReplaceAllString(stripScripts.ReplaceAllString(string(raw), " "), " ")), " ")
		return []evidence{{Title: input.URL, URL: input.URL, Excerpt: clip(strings.TrimSpace(text), 12000)}}, nil
	case "web_search":
		key := os.Getenv("BRAVE_SEARCH_API_KEY")
		if key == "" {
			return nil, errors.New("search_not_configured")
		}
		if strings.TrimSpace(input.Query) == "" || len(input.Query) > 1000 {
			return nil, errors.New("invalid_query")
		}
		raw, err := requestText(ctx, client, "https://api.search.brave.com/res/v1/web/search?q="+url.QueryEscape(input.Query)+"&count=5", map[string]string{"Accept": "application/json", "X-Subscription-Token": key})
		if err != nil {
			return nil, err
		}
		var response struct {
			Web struct {
				Results []struct{ Title, URL, Description string }
			}
		}
		if json.Unmarshal(raw, &response) != nil {
			return nil, errors.New("invalid_search_response")
		}
		var sources []evidence
		for _, result := range response.Web.Results {
			if len(sources) >= 5 {
				break
			}
			// Read original pages before allowing them to become report evidence.
			if _, err := validateURL(result.URL); err != nil {
				continue
			}
			pages, err := collectEvidence(ctx, client, toolInput{Tool: "fetch_page", URL: result.URL})
			if err == nil && len(pages) > 0 {
				pages[0].Title = clip(result.Title, 512)
				sources = append(sources, pages[0])
			}
		}
		if len(sources) == 0 {
			return nil, errors.New("no_readable_search_results")
		}
		return sources, nil
	case "github_repo":
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
			return nil, err
		}
		var metadata struct {
			Private       bool
			DefaultBranch string `json:"default_branch"`
		}
		if json.Unmarshal(raw, &metadata) != nil || metadata.Private || metadata.DefaultBranch == "" {
			return nil, errors.New("repository_not_public")
		}
		commitRaw, err := requestText(ctx, client, base+"/commits/"+url.PathEscape(metadata.DefaultBranch), headers)
		if err != nil {
			return nil, err
		}
		var commit struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(commitRaw, &commit) != nil || len(commit.SHA) != 40 {
			return nil, errors.New("invalid_commit")
		}
		headers["Accept"] = "application/vnd.github.raw+json"
		readme, err := requestText(ctx, client, base+"/readme?ref="+commit.SHA, headers)
		if err != nil {
			return nil, err
		}
		canonical := "https://github.com/" + input.Owner + "/" + input.Repo + "/tree/" + commit.SHA
		sources := []evidence{{Title: input.Owner + "/" + input.Repo + " README at " + commit.SHA, URL: canonical, Excerpt: clip(string(readme), 12000)}}
		headers["Accept"] = "application/vnd.github+json"
		directory, err := requestText(ctx, client, base+"/contents?ref="+commit.SHA, headers)
		if err != nil {
			return sources, nil
		}
		var entries []struct {
			Name string
			Type string
			Size int
		}
		if json.Unmarshal(directory, &entries) != nil {
			return sources, nil
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Type+" "+entry.Name)
		}
		sources = append(sources, evidence{Title: "Repository root at " + commit.SHA, URL: canonical, Excerpt: clip(strings.Join(names, "\n"), 4000)})
		allowedFiles := map[string]bool{"go.mod": true, "package.json": true, "Cargo.toml": true, "pyproject.toml": true, "requirements.txt": true}
		filesRead := 0
		for _, entry := range entries {
			if !allowedFiles[entry.Name] || entry.Type != "file" || entry.Size > 256*1024 || filesRead >= 5 {
				continue
			}
			headers["Accept"] = "application/vnd.github.raw+json"
			file, err := requestText(ctx, client, base+"/contents/"+url.PathEscape(entry.Name)+"?ref="+commit.SHA, headers)
			if err != nil {
				continue
			}
			filesRead++
			sources = append(sources, evidence{Title: entry.Name + " at " + commit.SHA, URL: "https://github.com/" + input.Owner + "/" + input.Repo + "/blob/" + commit.SHA + "/" + entry.Name, Excerpt: clip(string(file), 12000)})
		}
		return sources, nil
	default:
		return nil, errors.New("unknown_tool")
	}
}

func (e execution) saveSources(ctx context.Context, sources []evidence) ([]evidence, error) {
	err := e.mutate(ctx, func(tx *sql.Tx, _ taskRow) error {
		for i := range sources {
			source := &sources[i]
			source.ID = identifier(16)
			source.Title = clip(source.Title, 512)
			fingerprint := sha256.Sum256([]byte(source.URL + "\n" + source.Excerpt))
			var existing string
			err := tx.QueryRowContext(ctx, "SELECT source_id FROM research_sources WHERE tenant_id=? AND task_id=? AND fingerprint=?", e.Tenant, e.Task, fingerprint[:]).Scan(&existing)
			if err == nil {
				source.ID = existing
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO research_sources(tenant_id,task_id,source_id,title,canonical_url,excerpt,fingerprint) VALUES(?,?,?,?,?,?,?)", e.Tenant, e.Task, source.ID, source.Title, source.URL, source.Excerpt, fingerprint[:])
			if err != nil {
				return err
			}
		}
		return nil
	})
	return sources, err
}

func (a *apiServer) sources(w http.ResponseWriter, r *http.Request, tenant string) {
	var source evidence
	err := a.db.QueryRowContext(r.Context(), "SELECT source_id,title,canonical_url,excerpt FROM research_sources WHERE tenant_id=? AND task_id=? AND source_id=?", tenant, r.PathValue("id"), r.PathValue("source")).
		Scan(&source.ID, &source.Title, &source.URL, &source.Excerpt)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "source_not_found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, 200, source)
}

type reportDraft struct {
	Summary  string `json:"summary"`
	Findings []struct {
		Text    string   `json:"text"`
		Sources []string `json:"sources"`
	} `json:"findings"`
	Limitations []string `json:"limitations"`
}

func renderReport(draft reportDraft, sources []evidence) (string, []evidence, error) {
	byID := make(map[string]evidence)
	for _, s := range sources {
		byID[s.ID] = s
	}
	if len(draft.Findings) == 0 {
		return "", nil, errors.New("report_has_no_findings")
	}
	used := make([]evidence, 0)
	ordinals := map[string]int{}
	var body strings.Builder
	body.WriteString("# 研究报告\n\n")
	// Use plain, escaped text in Markdown to avoid unvalidated model URLs and HTML.
	escape := func(s string) string {
		for _, pair := range [][2]string{{"&", "&amp;"}, {"<", "&lt;"}, {">", "&gt;"}, {"[", "\\["}, {"]", "\\]"}, {"!", "\\!"}, {"(", "\\("}, {")", "\\)"}} {
			s = strings.ReplaceAll(s, pair[0], pair[1])
		}
		return s
	}
	for _, finding := range draft.Findings {
		if len(finding.Sources) == 0 {
			return "", nil, errors.New("finding_missing_citation")
		}
		body.WriteString(escape(finding.Text))
		for _, id := range finding.Sources {
			source, ok := byID[id]
			if !ok {
				return "", nil, errors.New("invalid_source_id")
			}
			ordinal := ordinals[id]
			if ordinal == 0 {
				used = append(used, source)
				ordinal = len(used)
				ordinals[id] = ordinal
			}
			body.WriteString(fmt.Sprintf(" [%d]", ordinal))
		}
		body.WriteString("\n\n")
	}
	body.WriteString("## 局限\n\n")
	for _, limitation := range draft.Limitations {
		body.WriteString("- " + escape(limitation) + "\n")
	}
	body.WriteString("\n## 参考资料\n\n")
	for i, source := range used {
		body.WriteString(fmt.Sprintf("%d. [%s](%s)\n", i+1, escape(source.Title), source.URL))
	}
	return body.String(), used, nil
}
