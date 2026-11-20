package research

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"researchpilot/internal/common/textutil"
	"researchpilot/internal/outbound"
	"researchpilot/internal/repository/model"
	toolcontract "researchpilot/internal/tools"
)

func webSearch(ctx context.Context, client *http.Client, input toolcontract.Input) ([]model.Source, error) {
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
	registry := NewRegistry(client)
	var sources []model.Source
	for _, result := range response.Web.Results {
		if len(sources) >= 5 {
			break
		}
		// Read original pages before allowing them to become report evidence.
		if _, err := outbound.ValidateURL(result.URL); err != nil {
			continue
		}
		pages, err := registry.Execute(ctx, toolcontract.Input{Tool: "fetch_page", URL: result.URL})
		if err == nil && len(pages) > 0 {
			pages[0].Title = textutil.Clip(result.Title, 512)
			sources = append(sources, pages[0])
		}
	}
	if len(sources) == 0 {
		return nil, errors.New("no_readable_search_results")
	}
	return sources, nil
}
