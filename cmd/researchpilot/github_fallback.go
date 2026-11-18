package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var commitID = regexp.MustCompile("^[a-f0-9]{40}$")
var githubState = regexp.MustCompile(`(?s)<script[^>]*data-target="react-app.embeddedData"[^>]*>(.*?)</script>`)

// This path carries no GitHub token. A public page alone must reveal a verified
// commit from its structured route state; arbitrary README text cannot pick SHA.
func githubPublicFallback(ctx context.Context, client *http.Client, input toolInput) ([]evidence, error) {
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
		return []evidence{{Title: input.Owner + "/" + input.Repo + " README at " + sha + " (public-page fallback; API metadata unavailable)", URL: base + "/blob/" + sha + "/" + url.PathEscape(name), Excerpt: clip(string(readme), 16000)}}, nil
	}
	return nil, errors.New("github_readme_unavailable")
}
