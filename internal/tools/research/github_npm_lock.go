package research

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"researchpilot/internal/common/textutil"
	toolcontract "researchpilot/internal/tools"
)

const maxNPMLockfileBytes = 1 << 20

type npmPackageRecord struct {
	Version              string
	Link                 bool
	Resolved             string
	Dependencies         map[string]string
	DevDependencies      map[string]string
	OptionalDependencies map[string]string
	PeerDependencies     map[string]string
}

func githubNPMLockExcerpt(raw []byte, goal string) string {
	var lock struct {
		LockfileVersion int
		Packages        map[string]npmPackageRecord
	}
	if json.Unmarshal(raw, &lock) != nil || (lock.LockfileVersion != 2 && lock.LockfileVersion != 3) {
		return "Lockfile summary unavailable: expected npm package-lock.json format 2 or 3."
	}
	root, ok := lock.Packages[""]
	if !ok {
		return "Lockfile summary unavailable: root package record is missing."
	}
	direct := make(map[string]string)
	for _, group := range []map[string]string{root.PeerDependencies, root.DevDependencies, root.Dependencies, root.OptionalDependencies} {
		for name, constraint := range group {
			direct[name] = constraint
		}
	}
	names := make([]string, 0, len(direct))
	for name := range direct {
		names = append(names, name)
	}
	slices.Sort(names)
	requested := make(map[string]bool)
	for _, token := range npmGoalToken.FindAllString(goal, -1) {
		requested[strings.TrimRight(token, ".")] = true
	}
	sort.SliceStable(names, func(i, j int) bool { return requested[names[i]] && !requested[names[j]] })
	var summary strings.Builder
	const maxRows = 40
	fmt.Fprintf(&summary, "npm lockfile version %d. Root direct dependency records (up to %d of %d). Versions describe the recorded dependency tree.\n",
		lock.LockfileVersion, min(maxRows, len(names)), len(names))
	for _, name := range names[:min(maxRows, len(names))] {
		record := lock.Packages["node_modules/"+name]
		switch {
		case record.Link:
			fmt.Fprintf(&summary, "%q declared %q; linked package\n", name, direct[name])
		case record.Version != "":
			fmt.Fprintf(&summary, "%q declared %q; locked version %q\n", name, direct[name], record.Version)
		default:
			fmt.Fprintf(&summary, "%q declared %q; root installation record unavailable\n", name, direct[name])
		}
	}
	return textutil.Clip(summary.String(), 4000) + npmTransitiveExcerpt(lock.Packages, names, requested)
}

var npmDependencyName = regexp.MustCompile(`^(@[a-zA-Z0-9_~-][a-zA-Z0-9._~-]*/)?[a-zA-Z0-9_~-][a-zA-Z0-9._~-]*$`)
var npmGoalToken = regexp.MustCompile(`[a-zA-Z0-9@_~./-]+`)

func npmTransitiveExcerpt(packages map[string]npmPackageRecord, direct []string, requested map[string]bool) string {
	type visit struct {
		location string
		chain    string
		depth    int
	}
	type dependencyPath struct {
		row       string
		requested bool
	}
	seen := make(map[string]bool)
	var queue []visit
	var links []string
	for location, record := range packages {
		name, rootModule := strings.CutPrefix(location, "node_modules/")
		if rootModule && record.Link && npmDependencyName.MatchString(name) && npmLocalTarget(packages, location) != "" {
			links = append(links, name)
		}
	}
	slices.Sort(links)
	var linkRows []string
	for _, name := range links[:min(12, len(links))] {
		target := npmLocalTarget(packages, "node_modules/"+name)
		linkRows = append(linkRows, fmt.Sprintf("%q -> %q", name, target))
	}
	for _, name := range append(slices.Clone(links), direct...) {
		modulePath := npmDependencyLocation(packages, "", name)
		location := npmLocalTarget(packages, modulePath)
		record := packages[location]
		if seen[location] || location == "" || record.Version == "" {
			continue
		}
		seen[location] = true
		queue = append(queue, visit{location, fmt.Sprintf("%q@%q", name, record.Version), 0})
	}
	const maxPaths = 24
	const maxDepth = 4
	const maxVisits = 120
	var paths []dependencyPath
	for i := 0; i < min(maxVisits, len(queue)) && len(paths) < maxVisits; i++ {
		parent := queue[i]
		if parent.depth == maxDepth {
			continue
		}
		record := packages[parent.location]
		names := make([]string, 0, len(record.Dependencies)+len(record.OptionalDependencies))
		for name := range record.Dependencies {
			names = append(names, name)
		}
		for name := range record.OptionalDependencies {
			if _, present := record.Dependencies[name]; !present {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			location := npmLocalTarget(packages, npmDependencyLocation(packages, parent.location, name))
			child := packages[location]
			if location == "" || seen[location] || child.Version == "" {
				continue
			}
			seen[location] = true
			chain := fmt.Sprintf("%s -> %q@%q", parent.chain, name, child.Version)
			paths = append(paths, dependencyPath{fmt.Sprintf("%s (lockfile path %q)", chain, location), requested[name]})
			queue = append(queue, visit{location, chain, parent.depth + 1})
			if len(paths) == maxVisits {
				break
			}
		}
	}
	var summary strings.Builder
	if len(linkRows) != 0 {
		fmt.Fprintf(&summary, "\nLocal package links (up to 12 of %d):\n%s\n",
			len(links), textutil.Clip(strings.Join(linkRows, "\n"), 900))
	}
	if len(paths) != 0 {
		sort.SliceStable(paths, func(i, j int) bool { return paths[i].requested && !paths[j].requested })
		rows := make([]string, 0, min(maxPaths, len(paths)))
		for _, sample := range paths[:min(maxPaths, len(paths))] {
			rows = append(rows, sample.row)
		}
		fmt.Fprintf(&summary, "\nSample dependency paths from the root project or local packages (regular and optional dependencies):\n%s",
			textutil.Clip(strings.Join(rows, "\n"), 2800))
	}
	return summary.String()
}

func npmLocalTarget(packages map[string]npmPackageRecord, location string) string {
	link := packages[location]
	if !link.Link {
		return location
	}
	target := strings.TrimPrefix(link.Resolved, "./")
	if path.Clean(target) != target || !toolcontract.ValidGitHubPath(target) || ignoredGitHubPath(target) {
		return ""
	}
	if record, ok := packages[target]; ok && !record.Link {
		return target
	}
	return ""
}

func npmDependencyLocation(packages map[string]npmPackageRecord, from, name string) string {
	if !npmDependencyName.MatchString(name) {
		return ""
	}
	// Check the package's own node_modules first, then ancestor directories.
	for directory := from; ; directory = path.Dir(directory) {
		if path.Base(directory) != "node_modules" {
			location := path.Join(directory, "node_modules", name)
			if _, ok := packages[location]; ok {
				return location
			}
		}
		if directory == "." || directory == "" {
			return ""
		}
	}
}
