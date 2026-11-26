package research

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/mod/modfile"

	"researchpilot/internal/common/textutil"
	toolcontract "researchpilot/internal/tools"
)

func githubManifestExcerpt(file string, raw []byte, limit int) string {
	summary := githubManifestSummary(file, raw)
	if len(summary) == 0 {
		return textutil.Clip(string(raw), limit)
	}
	const maxRows = 24
	listing := strings.Join(summary[:min(len(summary), maxRows)], "\n")
	header := fmt.Sprintf("Parsed manifest declarations (up to %d of %d entries; versions are declared constraints):\n%s\n\nManifest text:\n",
		min(len(summary), maxRows), len(summary), textutil.Clip(listing, 2500))
	return header + textutil.Clip(string(raw), limit-utf8.RuneCountInString(header))
}

func githubManifestSummary(file string, raw []byte) []string {
	var rows []string
	switch path.Base(file) {
	case "go.mod":
		parsed, err := modfile.Parse(file, raw, nil)
		if err != nil {
			return nil
		}
		if parsed.Module != nil {
			rows = append(rows, fmt.Sprintf("Go module %q", parsed.Module.Mod.Path))
		}
		for _, replacement := range parsed.Replace {
			if replacement.New.Version == "" {
				if target := relativeRepoDirectory(file, replacement.New.Path); target != "" {
					rows = append(rows, fmt.Sprintf("Local replacement %q -> %q", replacement.Old.Path, target))
				}
			}
		}
		for _, dependency := range parsed.Require {
			if !dependency.Indirect {
				rows = append(rows, fmt.Sprintf("Direct requirement %q %q", dependency.Mod.Path, dependency.Mod.Version))
			}
		}
	case "go.work":
		parsed, err := modfile.ParseWork(file, raw, nil)
		if err != nil {
			return nil
		}
		for _, member := range parsed.Use {
			if target := relativeRepoDirectory(file, member.Path); target != "" {
				rows = append(rows, fmt.Sprintf("Go workspace member %q", target))
			}
		}
	case "package.json":
		var pkg struct {
			Name                 string
			Dependencies         map[string]string
			DevDependencies      map[string]string
			PeerDependencies     map[string]string
			OptionalDependencies map[string]string
		}
		if json.Unmarshal(raw, &pkg) != nil {
			return nil
		}
		if pkg.Name != "" {
			rows = append(rows, fmt.Sprintf("JavaScript package %q", pkg.Name))
		}
		for _, group := range []struct {
			label string
			deps  map[string]string
		}{
			{"dependency", pkg.Dependencies},
			{"dev dependency", pkg.DevDependencies},
			{"peer dependency", pkg.PeerDependencies},
			{"optional dependency", pkg.OptionalDependencies},
		} {
			var declarations []string
			for name, version := range group.deps {
				if strings.HasPrefix(version, "workspace:") {
					declarations = append(declarations, fmt.Sprintf("Workspace %s %q %q", group.label, name, version))
				} else {
					declarations = append(declarations, fmt.Sprintf("Declared %s %q %q", group.label, name, version))
				}
			}
			slices.Sort(declarations)
			rows = append(rows, declarations...)
		}
	}
	return rows
}

func relativeRepoDirectory(file, target string) string {
	if target != "." && target != ".." && !strings.HasPrefix(target, "./") && !strings.HasPrefix(target, "../") {
		return ""
	}
	directory := path.Join(path.Dir(file), target)
	if directory == "." || toolcontract.ValidGitHubPath(directory) && !ignoredGitHubPath(directory) {
		return directory
	}
	return ""
}
