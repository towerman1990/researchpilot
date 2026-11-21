package research

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	evidencecontract "researchpilot/internal/evidence"
	"researchpilot/internal/repository/model"
	toolcontract "researchpilot/internal/tools"
)

var stripScripts = regexp.MustCompile("(?is)<(script|style)[^>]*>.*?</(script|style)>")
var articleContent = regexp.MustCompile("(?is)<article\\b[^>]*>(.*?)</article>")
var mainContent = regexp.MustCompile("(?is)<main\\b[^>]*>(.*?)</main>")
var navigationContent = regexp.MustCompile("(?is)<(?:nav|header|footer|aside|svg|noscript)\\b[^>]*>.*?</(?:nav|header|footer|aside|svg|noscript)>")
var stripTags = regexp.MustCompile("(?s)<[^>]+>")
var mysqlManualPage = regexp.MustCompile(`^/doc/refman/8[.]4/en/([a-z0-9][a-z0-9-]*[.]html)$`)
var spaceRuns = regexp.MustCompile("\\s+")

func fetchPage(ctx context.Context, client *http.Client, input toolcontract.Input) ([]model.Source, error) {
	location := input.URL
	u, parseErr := url.Parse(input.URL)
	if parseErr == nil && strings.EqualFold(u.Hostname(), "github.com") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 4 && parts[2] == "wiki" && repoPart.MatchString(parts[0]) && repoPart.MatchString(parts[1]) && regexp.MustCompile("^[A-Za-z0-9_.()-]{1,128}$").MatchString(parts[3]) {
			location = "https://raw.githubusercontent.com/wiki/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/" + url.PathEscape(parts[3]) + ".md"
		}
	}
	raw, err := requestText(ctx, client, location, nil)
	if err != nil && location != input.URL {
		raw, err = requestText(ctx, client, input.URL, nil)
	}
	sourceURL := input.URL
	// The MySQL site can deny automated clients. Oracle publishes the same
	// version of the manual, so retry the matching public page once.
	if err != nil && err.Error() == "upstream_http_403" && parseErr == nil {
		if mirror := mysqlManualMirror(u); mirror != "" {
			if mirrored, mirrorErr := requestText(ctx, client, mirror, nil); mirrorErr == nil {
				raw, err, sourceURL = mirrored, nil, mirror
			}
		}
	}
	if err != nil {
		return nil, err
	}
	text := pageTextAt(string(raw), sourceURL)
	goal, _ := ctx.Value(evidenceGoalKey{}).(string)
	if parseErr == nil {
		goal += " " + strings.ReplaceAll(u.Fragment, ".", " ")
	}
	text = evidencecontract.Select(text, goal, 18000)
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty_page")
	}
	return []model.Source{{Title: sourceURL, URL: sourceURL, Excerpt: strings.TrimSpace(text)}}, nil
}

func mysqlManualMirror(u *url.URL) string {
	if u.Scheme != "https" || !strings.EqualFold(u.Host, "dev.mysql.com") || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return ""
	}
	match := mysqlManualPage.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return ""
	}
	mirror := url.URL{
		Scheme:   "https",
		Host:     "docs.oracle.com",
		Path:     "/cd/E17952_01/mysql-8.4-en/" + match[1],
		Fragment: u.Fragment,
	}
	return mirror.String()
}

func pageText(raw string) string {
	raw = stripScripts.ReplaceAllString(raw, " ")
	if article := articleContent.FindStringSubmatch(raw); len(article) > 1 {
		raw = article[1]
	} else if main := mainContent.FindStringSubmatch(raw); len(main) > 1 {
		raw = main[1]
	}
	raw = navigationContent.ReplaceAllString(raw, " ")
	return strings.TrimSpace(spaceRuns.ReplaceAllString(html.UnescapeString(stripTags.ReplaceAllString(raw, " ")), " "))
}

func pageTextAt(raw, location string) string {
	u, err := url.Parse(location)
	if err == nil && u.Fragment != "" {
		anchor := regexp.MustCompile("(?is)\\bid\\s*=\\s*(?:[\"']" + regexp.QuoteMeta(u.Fragment) + "[\"']|" + regexp.QuoteMeta(u.Fragment) + "(?:[\\s>]))")
		if loc := anchor.FindStringIndex(raw); loc != nil {
			start := strings.LastIndex(raw[:loc[0]], "<")
			if start < 0 {
				start = loc[0]
			}
			raw = raw[start:]
		}
	}
	return pageText(raw)
}
