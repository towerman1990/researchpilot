package research

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestToolCredentialsNeverFollowRedirects(t *testing.T) {
	for _, header := range []string{"Authorization", "X-Subscription-Token"} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example.com/collect"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		})}
		if _, err := requestText(context.Background(), client, "https://api.example.com/data", map[string]string{header: "fixture-secret"}); err == nil || calls != 1 {
			t.Fatal("credential redirect followed", header, calls, err)
		}
	}
}
