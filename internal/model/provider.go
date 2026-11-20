package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}
type Request struct {
	Model          string    `json:"model"`
	Messages       []Message `json:"messages"`
	MaxTokens      int       `json:"max_tokens"`
	Tools          []Tool    `json:"tools,omitempty"`
	ToolChoice     any       `json:"tool_choice,omitempty"`
	ResponseFormat any       `json:"response_format,omitempty"`
	Stream         bool      `json:"stream,omitempty"`
	Thinking       any       `json:"thinking,omitempty"`
	StreamOptions  any       `json:"stream_options,omitempty"`
}
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type Response struct {
	Text      string
	Calls     []ToolCall
	Tokens    uint32
	Finish    string
	RequestID string
	Breakdown *TokenBreakdown
}
type TokenBreakdown struct{ Input, Output uint32 }

type wireUsage struct {
	Total  int64           `json:"total_tokens"`
	Input  json.RawMessage `json:"prompt_tokens"`
	Output json.RawMessage `json:"completion_tokens"`
}

func (u wireUsage) breakdown() *TokenBreakdown {
	var input, output *int64
	if json.Unmarshal(u.Input, &input) != nil || json.Unmarshal(u.Output, &output) != nil ||
		input == nil || output == nil || *input < 0 || *output < 0 ||
		*input > u.Total || *output > u.Total || *input+*output != u.Total {
		return nil
	}
	return &TokenBreakdown{Input: uint32(*input), Output: uint32(*output)}
}

type Event struct{ Text string }
type Provider interface {
	Generate(context.Context, Request) (Response, error)
	Stream(context.Context, Request, func(Event) error) (Response, error)
}

// HTTP errors expose only status and bounded retry timing, never upstream bodies.
type HTTPError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("model_http_%d", e.Status) }
func (e *HTTPError) Retryable() bool {
	switch e.Status {
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

type Compatible struct {
	Client       *http.Client
	BaseURL, Key string
}

func (p Compatible) request(ctx context.Context, r Request) (*http.Response, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid_model_request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.Key)
	res, err := p.Client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "private_address_forbidden") {
			return nil, errors.New("model_dns_policy_denied")
		}
		if strings.Contains(err.Error(), "dns_resolution_failed") || strings.Contains(err.Error(), "dns_response_invalid") {
			return nil, errors.New("model_dns_resolution_failed")
		}
		if strings.Contains(err.Error(), "invalid_outbound_dns_config") {
			return nil, errors.New("invalid_outbound_dns_config")
		}
		if strings.Contains(err.Error(), "x509:") {
			return nil, errors.New("model_tls_failed")
		}
		return nil, errors.New("model_connection_failed")
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		delay := time.Second
		raw := res.Header.Get("Retry-After")
		if n, e := strconv.Atoi(raw); e == nil && n > 0 {
			delay = time.Duration(n) * time.Second
		} else if t, e := http.ParseTime(raw); e == nil && time.Until(t) > 0 {
			delay = time.Until(t)
		}
		if delay > time.Minute {
			delay = time.Minute
		}
		return nil, &HTTPError{res.StatusCode, delay}
	}
	return res, nil
}
func validate(r Response) error {
	if r.Finish == "length" {
		return errors.New("model_output_truncated")
	}
	if r.Finish != "stop" && r.Finish != "tool_calls" {
		return errors.New("incomplete_model_response")
	}
	if r.Finish == "tool_calls" && len(r.Calls) == 0 {
		return errors.New("missing_tool_calls")
	}
	ids := map[string]bool{}
	for _, c := range r.Calls {
		var args map[string]json.RawMessage
		if c.ID == "" || ids[c.ID] || c.Type != "function" || c.Function.Name == "" || json.Unmarshal([]byte(c.Function.Arguments), &args) != nil || args == nil {
			return errors.New("invalid_tool_call")
		}
		ids[c.ID] = true
	}
	if len(r.Calls) > 0 && r.Finish != "tool_calls" {
		return errors.New("invalid_tool_finish")
	}
	return nil
}
func (p Compatible) Generate(ctx context.Context, r Request) (Response, error) {
	r.Stream = false
	r.StreamOptions = nil
	res, err := p.request(ctx, r)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return Response{}, errors.New("model_response_too_large_or_interrupted")
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string     `json:"content"`
				Calls   []ToolCall `json:"tool_calls"`
			} `json:"message"`
			Finish string `json:"finish_reason"`
		} `json:"choices"`
		Usage wireUsage `json:"usage"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Choices) != 1 || envelope.Usage.Total < 0 || envelope.Usage.Total > 1<<31 {
		return Response{}, errors.New("invalid_model_response")
	}
	c := envelope.Choices[0]
	out := Response{Text: c.Message.Content, Calls: c.Message.Calls, Tokens: uint32(envelope.Usage.Total),
		Finish: c.Finish, RequestID: res.Header.Get("x-request-id"), Breakdown: envelope.Usage.breakdown()}
	if err = validate(out); err != nil {
		return Response{Tokens: out.Tokens, Finish: out.Finish, RequestID: out.RequestID, Breakdown: out.Breakdown}, err
	}
	return out, nil
}

// Tool fragments never leave this adapter. Only a valid finish followed by DONE
// publishes completed calls; EOF, overflow and malformed arguments discard them.
func (p Compatible) Stream(ctx context.Context, r Request, emit func(Event) error) (Response, error) {
	r.Stream = true
	r.StreamOptions = map[string]bool{"include_usage": true}
	res, err := p.request(ctx, r)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	scan := bufio.NewScanner(io.LimitReader(res.Body, (2<<20)+1))
	scan.Buffer(make([]byte, 4096), 2<<20)
	out := Response{RequestID: res.Header.Get("x-request-id")}
	calls := map[int]*ToolCall{}
	total := 0
	done := false
	for scan.Scan() {
		line := scan.Text()
		total += len(line) + 1
		if total > 2<<20 {
			return Response{}, errors.New("model_stream_too_large")
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		if data == "" {
			continue
		}
		var part struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content string `json:"content"`
					Calls   []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				Finish *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *wireUsage `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &part) != nil {
			return Response{}, errors.New("invalid_model_stream")
		}
		if part.Usage != nil {
			if part.Usage.Total < 0 || part.Usage.Total > 1<<31 {
				return Response{}, errors.New("invalid_model_usage")
			}
			out.Tokens = uint32(part.Usage.Total)
			out.Breakdown = part.Usage.breakdown()
		}
		if len(part.Choices) > 1 {
			return Response{}, errors.New("invalid_model_choices")
		}
		for _, c := range part.Choices {
			if c.Index != 0 || out.Finish != "" {
				return Response{}, errors.New("invalid_model_stream_order")
			}
			out.Text += c.Delta.Content
			if emit != nil && c.Delta.Content != "" {
				if err := emit(Event{Text: c.Delta.Content}); err != nil {
					return Response{}, err
				}
			}
			for _, fragment := range c.Delta.Calls {
				if fragment.Index < 0 || fragment.Index >= 16 {
					return Response{}, errors.New("invalid_tool_index")
				}
				call := calls[fragment.Index]
				if call == nil {
					call = &ToolCall{}
					calls[fragment.Index] = call
				}
				if fragment.ID != "" {
					if call.ID != "" && call.ID != fragment.ID {
						return Response{}, errors.New("tool_id_changed")
					}
					call.ID = fragment.ID
				}
				if fragment.Type != "" {
					call.Type = fragment.Type
				}
				call.Function.Name += fragment.Function.Name
				call.Function.Arguments += fragment.Function.Arguments
			}
			if c.Finish != nil {
				out.Finish = *c.Finish
			}
		}
	}
	if scan.Err() != nil || !done {
		return Response{}, errors.New("model_stream_interrupted")
	}
	for i := 0; i < len(calls); i++ {
		if calls[i] == nil {
			return Response{}, errors.New("missing_tool_index")
		}
		out.Calls = append(out.Calls, *calls[i])
	}
	if err = validate(out); err != nil {
		return Response{}, err
	}
	return out, nil
}
