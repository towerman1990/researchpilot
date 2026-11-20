package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fixture(t *testing.T, body string, status int) Compatible {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth")
		}
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return Compatible{Client: server.Client(), BaseURL: server.URL, Key: "test-key"}
}
func chunk(delta any, finish any) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
	return "data: " + string(b) + "\n\n"
}
func toolFragment(index int, id, name, args string) map[string]any {
	return map[string]any{"tool_calls": []any{map[string]any{"index": index, "id": id, "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}
}
func TestStreamAssemblesInterleavedCalls(t *testing.T) {
	body := chunk(toolFragment(0, "one", "fetch_page", `{"url":`), nil) + chunk(toolFragment(1, "two", "web_search", `{"query":"go"}`), nil) + chunk(toolFragment(0, "", "", `"https://go.dev"}`), nil) + chunk(map[string]any{}, "tool_calls") + "data: {\"choices\":[],\"usage\":{\"total_tokens\":17}}\n\ndata: [DONE]\n\n"
	out, err := fixture(t, body, 200).Stream(context.Background(), Request{}, nil)
	if err != nil || len(out.Calls) != 2 || out.Calls[0].Function.Arguments != `{"url":"https://go.dev"}` || out.Tokens != 17 {
		t.Fatalf("%+v %v", out, err)
	}
}
func TestStreamNeverPublishesIncompleteCalls(t *testing.T) {
	initial := chunk(toolFragment(0, "one", "fetch_page", `{"url":`), nil)
	tests := []string{initial, initial + "data: [DONE]\n\n", initial + chunk(map[string]any{}, "length") + "data: [DONE]\n\n", initial + chunk(map[string]any{}, "tool_calls") + "data: [DONE]\n\n", initial + "data: invalid\n\n"}
	for i, body := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			out, err := fixture(t, body, 200).Stream(context.Background(), Request{}, nil)
			if err == nil || len(out.Calls) != 0 {
				t.Fatal("partial calls escaped", out, err)
			}
		})
	}
}
func TestStreamRejectsChangedIdentity(t *testing.T) {
	body := chunk(toolFragment(0, "one", "fetch_page", ""), nil) + chunk(toolFragment(0, "two", "", `{}`), nil) + chunk(map[string]any{}, "tool_calls") + "data: [DONE]\n\n"
	if _, err := fixture(t, body, 200).Stream(context.Background(), Request{}, nil); err == nil {
		t.Fatal("identity changed")
	}
}
func TestHTTPFailureClassification(t *testing.T) {
	for _, status := range []int{401, 403, 408, 429, 500, 502, 503, 504} {
		p := fixture(t, "credential-must-not-leak", status)
		_, err := p.Generate(context.Background(), Request{})
		e, ok := err.(*HTTPError)
		if !ok || e.RetryAfter.Seconds() != 7 || e.Retryable() != (status != 401 && status != 403) || strings.Contains(err.Error(), "credential") {
			t.Fatal(status, err)
		}
	}
}
func TestGenerateRejectsInvalidFinishAndUsage(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"message":{"content":"{}"},"finish_reason":"length"}]}`,
		`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"total_tokens":-1}}`,
		`{"choices":[{"message":{"tool_calls":[{"id":"a","type":"function","function":{"name":"fetch","arguments":"{"}}]},"finish_reason":"tool_calls"}]}`,
	} {
		if _, err := fixture(t, body, 200).Generate(context.Background(), Request{}); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}
func TestStreamTextAndCancellation(t *testing.T) {
	body := chunk(map[string]string{"content": "hello"}, nil) + chunk(map[string]any{}, "stop") + "data: [DONE]\n\n"
	text := ""
	out, err := fixture(t, body, 200).Stream(context.Background(), Request{}, func(e Event) error { text += e.Text; return nil })
	if err != nil || out.Text != "hello" || text != "hello" {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture(t, body, 200).Stream(ctx, Request{}, nil); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestDNSPolicyFailureIsExplicit(t *testing.T) {
	p := Compatible{BaseURL: "https://api.deepseek.com", Key: "fixture", Client: &http.Client{Transport: errorTransport{}}}
	_, err := p.Generate(context.Background(), Request{})
	if err == nil || err.Error() != "model_dns_policy_denied" {
		t.Fatal(err)
	}
}

type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private_address_forbidden")
}

func TestTruncatedCompletionKeepsKnownUsage(t *testing.T) {
	p := fixture(t, `{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}],"usage":{"total_tokens":99}}`, 200)
	out, err := p.Generate(context.Background(), Request{})
	if err == nil || err.Error() != "model_output_truncated" || out.Tokens != 99 || out.Text != "" || len(out.Calls) != 0 {
		t.Fatal(out, err)
	}
}

func TestProviderTokenBreakdown(t *testing.T) {
	for _, test := range []struct {
		usage string
		want  *TokenBreakdown
	}{
		{`"total_tokens":17,"prompt_tokens":12,"completion_tokens":5`, &TokenBreakdown{12, 5}},
		{`"total_tokens":17`, nil},
		{`"total_tokens":17,"prompt_tokens":12,"completion_tokens":7`, nil},
		{`"total_tokens":17,"prompt_tokens":"12","completion_tokens":5`, nil},
	} {
		body := `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{` + test.usage + `}}`
		out, err := fixture(t, body, 200).Generate(t.Context(), Request{})
		if err != nil || out.Tokens != 17 || (out.Breakdown == nil) != (test.want == nil) ||
			(test.want != nil && *out.Breakdown != *test.want) {
			t.Fatalf("usage %s: %+v, %v", test.usage, out, err)
		}
	}
}
