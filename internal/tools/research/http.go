package research

import (
	"context"
	"errors"
	"net/http"

	"researchpilot/internal/outbound"
)

func requestText(ctx context.Context, client *http.Client, raw string, headers map[string]string) ([]byte, error) {
	return requestTextLimit(ctx, client, raw, headers, 2<<20)
}
func requestTextLimit(ctx context.Context, client *http.Client, raw string, headers map[string]string, limit int64) ([]byte, error) {
	if _, err := outbound.ValidateURL(raw); err != nil {
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
	requestClient := client
	if headers["Authorization"] != "" || headers["X-Subscription-Token"] != "" {
		isolated := *client
		isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("credential_redirect_forbidden") }
		requestClient = &isolated
	}
	response, err := requestClient.Do(request)
	if err != nil {
		return nil, errors.New("fetch_failed")
	}
	return outbound.ReadLimited(response, limit)
}
