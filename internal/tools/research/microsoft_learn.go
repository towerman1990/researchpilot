package research

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"researchpilot/internal/common/textutil"
	"researchpilot/internal/outbound"
	"researchpilot/internal/repository/model"
	toolcontract "researchpilot/internal/tools"
)

const learnEndpoint = "https://learn.microsoft.com/api/mcp"
const learnProtocol = "2025-06-18"
const maxLearnResponse = 512 << 10

type learnSession struct {
	client           *http.Client
	version, session string
}

type learnReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func microsoftDocsSearch(ctx context.Context, client *http.Client, input toolcontract.Input) ([]model.Source, error) {
	publicPhrase := toolcontract.MicrosoftDocsQuery(input.Query)
	if publicPhrase == "" {
		return nil, errors.New("mcp_unknown_public_topic")
	}
	fixedClient := *client
	fixedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := learnSession{client: &fixedClient}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"capabilities"`
	}
	if err := c.post(ctx, 1, "initialize", map[string]any{
		"protocolVersion": learnProtocol,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "researchpilot", "version": "1"},
	}, &init); err != nil {
		return nil, err
	}
	switch init.ProtocolVersion {
	case "2025-03-26", learnProtocol, "2025-11-25":
		c.version = init.ProtocolVersion
	default:
		return nil, errors.New("mcp_protocol_unsupported")
	}
	defer c.close(ctx)
	if len(init.Capabilities.Tools) == 0 || bytes.Equal(init.Capabilities.Tools, []byte("null")) {
		return nil, errors.New("mcp_tools_unavailable")
	}
	if err := c.post(ctx, 0, "notifications/initialized", nil, nil); err != nil {
		return nil, err
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := c.post(ctx, 2, "tools/call", map[string]any{
		"name":      "microsoft_docs_search",
		"arguments": map[string]string{"query": publicPhrase},
	}, &result); err != nil {
		return nil, err
	}
	if result.IsError {
		return nil, errors.New("mcp_tool_failed")
	}
	sources := make([]model.Source, 0, 5)
	seen := map[string]bool{}
	for _, block := range result.Content {
		var page struct {
			Results []struct {
				Title   string `json:"title"`
				URL     string `json:"contentUrl"`
				Excerpt string `json:"content"`
			} `json:"results"`
		}
		if block.Type != "text" || json.Unmarshal([]byte(block.Text), &page) != nil {
			continue
		}
		for _, item := range page.Results {
			u, err := outbound.ValidateURL(item.URL)
			if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "learn.microsoft.com") ||
				(u.Port() != "" && u.Port() != "443") || len(item.URL) > 2048 || seen[item.URL] ||
				strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.Excerpt) == "" {
				continue
			}
			seen[item.URL] = true
			sources = append(sources, model.Source{Title: textutil.Clip(item.Title, 250), URL: item.URL, Excerpt: textutil.Clip(item.Excerpt, 2400)})
			if len(sources) == cap(sources) {
				return sources, nil
			}
		}
	}
	if len(sources) == 0 {
		return nil, errors.New("mcp_no_document_results")
	}
	return sources, nil
}

func (c learnSession) request(ctx context.Context, method string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, learnEndpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("mcp_connection_failed")
	}
	return res, nil
}

func (c learnSession) close(ctx context.Context) {
	if c.session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if res, err := c.request(ctx, http.MethodDelete, nil); err == nil {
		res.Body.Close()
	}
}

func (c *learnSession) post(ctx context.Context, id int, method string, params, target any) error {
	request := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != 0 {
		request["id"], request["params"] = id, params
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	res, err := c.request(ctx, http.MethodPost, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if id == 0 && res.StatusCode == http.StatusAccepted {
		return nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("mcp_http_%d", res.StatusCode)
	}
	reply, err := readLearnReply(res, id)
	if err != nil {
		return err
	}
	if reply.JSONRPC != "2.0" || reply.ID != id || len(reply.Error) != 0 ||
		len(reply.Result) == 0 || bytes.Equal(reply.Result, []byte("null")) || json.Unmarshal(reply.Result, target) != nil {
		return errors.New("mcp_invalid_reply")
	}
	if method == "initialize" {
		session := res.Header.Get("Mcp-Session-Id")
		if len(session) > 256 || strings.IndexFunc(session, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
			return errors.New("mcp_invalid_session")
		}
		c.session = session
	}
	return nil
}

func readLearnReply(res *http.Response, id int) (learnReply, error) {
	var reply learnReply
	body := io.LimitReader(res.Body, maxLearnResponse+1)
	media, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	switch media {
	case "application/json":
		raw, err := io.ReadAll(body)
		if err == nil && len(raw) <= maxLearnResponse && json.Unmarshal(raw, &reply) == nil {
			return reply, nil
		}
	case "text/event-stream":
		scan := bufio.NewScanner(body)
		scan.Buffer(make([]byte, 4096), maxLearnResponse)
		var data []string
		for scan.Scan() {
			line := scan.Text()
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(line[5:], " "))
			} else if line == "" && len(data) > 0 {
				reply = learnReply{}
				if json.Unmarshal([]byte(strings.Join(data, "\n")), &reply) == nil && reply.ID == id &&
					(len(reply.Result) > 0 || len(reply.Error) > 0) {
					return reply, nil
				}
				data = data[:0]
			}
		}
	}
	return learnReply{}, errors.New("mcp_invalid_response")
}
