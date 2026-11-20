package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"researchpilot/internal/common/textutil"
	"researchpilot/internal/repository/model"
	toolcontract "researchpilot/internal/tools"
)

type gitTreeEntry struct {
	Path string
	Type string
	Mode string
	SHA  string
	Size int
}

type githubTree struct {
	Tree      []gitTreeEntry
	Truncated bool
}

func readGitHubTree(ctx context.Context, client *http.Client, api, sha string, recursive bool, headers map[string]string) (githubTree, error) {
	endpoint := api + "/git/trees/" + sha
	if recursive {
		endpoint += "?recursive=1"
	}
	body, err := requestText(ctx, client, endpoint, headers)
	if err != nil {
		return githubTree{}, err
	}
	var tree githubTree
	if err := json.Unmarshal(body, &tree); err != nil || tree.Tree == nil {
		return githubTree{}, errors.New("invalid_github_tree")
	}
	return tree, nil
}

func githubTreeSources(ctx context.Context, client *http.Client, api, repo, commit, root, scope string, headers map[string]string) ([]model.Source, error) {
	sha := root
	if scope != "" {
		for _, part := range strings.Split(scope, "/") {
			level, err := readGitHubTree(ctx, client, api, sha, false, headers)
			if err != nil {
				return nil, err
			}
			sha = ""
			for _, entry := range level.Tree {
				if entry.Path == part && entry.Type == "tree" && commitID.MatchString(entry.SHA) {
					sha = entry.SHA
					break
				}
			}
			if sha == "" {
				if level.Truncated {
					return nil, errors.New("github_scoped_listing_unavailable")
				}
				return nil, errors.New("github_directory_not_found")
			}
		}
	}
	tree, err := readGitHubTree(ctx, client, api, sha, true, headers)
	shallow := err != nil || tree.Truncated
	if shallow {
		tree, err = readGitHubTree(ctx, client, api, sha, false, headers)
		if err != nil {
			return nil, err
		}
	}

	const maxDirectoryLines = 72
	const maxManifestReads = 6
	directories := make([]string, 0, len(tree.Tree))
	files := make([]string, 0, len(tree.Tree))
	manifests := make([]gitTreeEntry, 0)
	for _, entry := range tree.Tree {
		if !toolcontract.ValidGitHubPath(entry.Path) || ignoredGitHubPath(entry.Path) {
			continue
		}
		if entry.Type == "tree" {
			directories = append(directories, entry.Path+"/")
		}
		if entry.Type == "blob" && (entry.Mode == "100644" || entry.Mode == "100755") {
			files = append(files, entry.Path)
			if entry.Size <= 256<<10 && dependencyManifest(entry.Path) {
				manifests = append(manifests, entry)
			}
		}
	}
	slices.SortFunc(directories, compareRepoPaths)
	slices.SortFunc(files, compareRepoPaths)
	slices.SortFunc(manifests, func(a, b gitTreeEntry) int { return compareRepoPaths(a.Path, b.Path) })

	rawHeaders := make(map[string]string, len(headers))
	for key, value := range headers {
		rawHeaders[key] = value
	}
	rawHeaders["Accept"] = "application/vnd.github.raw+json"
	titleScope := scope
	canonical := repo + "/tree/" + commit
	if scope == "" {
		titleScope = "/"
	} else {
		canonical += "/" + escapeGitHubPath(scope)
	}
	sources := []model.Source{{Title: "Repository directory " + titleScope + " at " + commit, URL: canonical}}
	var evidence strings.Builder
	fmt.Fprintf(&evidence, "Directory scope: %s\nCommit: %s\n", titleScope, commit)
	if shallow || tree.Truncated {
		evidence.WriteString("Only the immediate directory was returned; nested paths may be absent.\n")
	}
	manifestPaths := make([]string, 0, min(len(manifests), maxDirectoryLines))
	for i, entry := range manifests[:min(len(manifests), maxDirectoryLines)] {
		file := entry.Path
		if scope != "" {
			file = scope + "/" + file
		}
		manifestPaths = append(manifestPaths, file)
		if i >= maxManifestReads || !toolcontract.ValidGitHubPath(file) {
			continue
		}
		escaped := escapeGitHubPath(file)
		raw, err := requestTextLimit(ctx, client, api+"/contents/"+escaped+"?ref="+commit, rawHeaders, 256<<10)
		if err != nil {
			continue
		}
		sources = append(sources, model.Source{
			Title:   "Dependency manifest " + file + " at " + commit,
			URL:     repo + "/blob/" + commit + "/" + escaped,
			Excerpt: textutil.Clip(string(raw), 10000),
		})
	}
	fmt.Fprintf(&evidence, "Manifest files read: %d of %d candidates (up to %d).\n", len(sources)-1, len(manifests), maxManifestReads)
	writePathSummary(&evidence, "Dependency manifests", manifestPaths, len(manifests), 2000)
	writePathSummary(&evidence, "Directories", directories[:min(len(directories), maxDirectoryLines)], len(directories), 2500)
	writePathSummary(&evidence, "Files", files[:min(len(files), maxDirectoryLines)], len(files), 2500)
	sources[0].Excerpt = evidence.String()
	return sources, nil
}

func writePathSummary(out *strings.Builder, label string, paths []string, total, limit int) {
	var listing strings.Builder
	count := 0
	for _, entry := range paths {
		if listing.Len()+len(entry)+1 > limit {
			break
		}
		fmt.Fprintln(&listing, entry)
		count++
	}
	fmt.Fprintf(out, "%s shown (%d of %d, filtered):\n%s", label, count, total, listing.String())
}

func compareRepoPaths(a, b string) int {
	depthA, depthB := strings.Count(a, "/"), strings.Count(b, "/")
	if depthA != depthB {
		return depthA - depthB
	}
	return strings.Compare(a, b)
}

func dependencyManifest(file string) bool {
	switch path.Base(file) {
	case "go.mod", "go.work", "package.json", "Cargo.toml", "pyproject.toml", "requirements.txt", "pom.xml", "build.gradle", "build.gradle.kts":
		return true
	default:
		return false
	}
}

func ignoredGitHubPath(file string) bool {
	for _, part := range strings.Split(file, "/") {
		switch part {
		case "vendor", "node_modules", "third_party", ".git":
			return true
		}
	}
	return false
}

func escapeGitHubPath(file string) string {
	parts := strings.Split(file, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
