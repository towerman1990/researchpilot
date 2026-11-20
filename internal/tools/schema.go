package tools

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Input struct {
	Tool  string `json:"tool"`
	Query string `json:"query,omitempty"`
	URL   string `json:"url,omitempty"`
	Owner string `json:"owner,omitempty"`
	Repo  string `json:"repo,omitempty"`
	Path  string `json:"path,omitempty"`
}
type Error struct {
	Code                   string
	Correctable, Retryable bool
}

func (e *Error) Error() string { return e.Code }

var repositoryPart = regexp.MustCompile("^[A-Za-z0-9_.-]{1,100}$")

// ValidGitHubPath accepts a bounded, repository-relative path.
func ValidGitHubPath(value string) bool {
	if value == "" || len(value) > 400 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\%") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) > 8 {
		return false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || len(part) > 100 || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (i Input) Validate() error {
	invalid := func() error { return &Error{Code: "invalid_arguments", Correctable: true} }
	switch i.Tool {
	case "fetch_page":
		u, err := url.Parse(i.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || len(i.URL) > 2048 || i.Query != "" || i.Owner != "" || i.Repo != "" || i.Path != "" {
			return invalid()
		}
	case "web_search":
		if strings.TrimSpace(i.Query) == "" || len(i.Query) > 1000 || i.URL != "" || i.Owner != "" || i.Repo != "" || i.Path != "" {
			return invalid()
		}
	case "github_repo":
		if !repositoryPart.MatchString(i.Owner) || !repositoryPart.MatchString(i.Repo) || i.Owner == "." || i.Owner == ".." || i.Repo == "." || i.Repo == ".." || (i.Path != "" && !ValidGitHubPath(i.Path)) || i.URL != "" || i.Query != "" {
			return invalid()
		}
	default:
		return &Error{Code: "unknown_tool", Correctable: true}
	}
	return nil
}

// The same published input schemas are available to provider capability clients.
var Schemas = map[string]json.RawMessage{
	"fetch_page":  json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","maxLength":2048}},"required":["url"],"additionalProperties":false}`),
	"web_search":  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":1000}},"required":["query"],"additionalProperties":false}`),
	"github_repo": json.RawMessage(`{"type":"object","properties":{"owner":{"type":"string","pattern":"^[A-Za-z0-9_.-]{1,100}$"},"repo":{"type":"string","pattern":"^[A-Za-z0-9_.-]{1,100}$"},"path":{"type":"string","maxLength":400}},"required":["owner","repo"],"additionalProperties":false}`),
}
