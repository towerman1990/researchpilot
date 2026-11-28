package execution

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestReportBudgetReservationIntegration(t *testing.T) {
	db := integrationDB(t)
	e, _ := claimTest(t, db)
	var wg sync.WaitGroup
	results := make(chan error, 3)
	for range 3 {
		wg.Go(func() { _, err := e.reserve(t.Context(), 10000, reportBudgetReserve); results <- err })
	}
	wg.Wait()
	accepted := 0
	for range 3 {
		err := <-results
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrBudget) {
			t.Fatal(err)
		}
	}
	var reserved uint32
	if err := db.QueryRow("SELECT tokens_reserved FROM research_tasks WHERE id=?", e.Task).Scan(&reserved); err != nil || accepted != 2 || reserved != 20000 {
		t.Fatalf("protected report budget: accepted=%d reserved=%d error=%v", accepted, reserved, err)
	}
}

// Reproduce the production case: 22,787 charged tokens, a completed draft,
// and enough remaining budget for a compact boolean citation review.
func TestCitationReviewRemainingBudgetIntegration(t *testing.T) {
	db := integrationDB(t)
	if os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
		t.Skip("MODEL_MASTER_KEY_FILE not configured")
	}
	e, task := claimTest(t, db)
	a := apiServer{db: db}
	rec := httptest.NewRecorder()
	a.modelConfig(rec, httptest.NewRequest("PUT", "/model-config", strings.NewReader(`{"base_url":"https://api.deepseek.com","model":"fixture","api_key":"fixture-key"}`)), e.Tenant)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	config, err := loadModel(t.Context(), db, e.Tenant, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE research_tasks SET tokens_used=22787 WHERE id=?", e.Task); err != nil {
		t.Fatal(err)
	}
	task.reviewCitations = true
	state := &researchState{task: task, config: config}
	for i := range 3 {
		id := fmtSeq(uint64(i + 1))
		state.sources = append(state.sources, evidence{ID: id, Title: "Official evidence", URL: "https://example.com/docs", Excerpt: strings.Repeat("Reliable execution is documented. ", 800)})
		state.draft.Findings = append(state.draft.Findings, struct {
			Text    string   `json:"text"`
			Sources []string `json:"sources"`
		}{Text: strings.Repeat("Reliable execution. ", 80), Sources: []string{id}})
	}
	state.report, state.citations, err = renderReport(state.draft, state.sources)
	if err != nil {
		t.Fatal(err)
	}
	full := citationReviewPrompt(state)
	if len(full)+1024+256 <= 17213 {
		t.Fatal("fixture does not require excerpt compression")
	}
	calls := 0
	e.Runner.modelHTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var request struct {
			MaxTokens int `json:"max_tokens"`
			Messages  []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		prompt := request.Messages[1].Content
		if request.MaxTokens != 256 || len(prompt)+1024+request.MaxTokens > 17213 || prompt == full {
			t.Fatalf("review did not fit its actual reservation: max_tokens=%d input_bytes=%d", request.MaxTokens, len(prompt))
		}
		for _, finding := range state.draft.Findings {
			if !strings.Contains(prompt, finding.Text) || !strings.Contains(prompt, finding.Sources[0]) {
				t.Fatal("compression removed a finding or citation ID")
			}
		}
		body := `{"choices":[{"message":{"content":"{\"supported\":[true,true,true]}"},"finish_reason":"stop"}],"usage":{"total_tokens":100}}`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	if _, err := e.reviewResearch(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	var used, reserved int
	if err := db.QueryRow("SELECT tokens_used,tokens_reserved FROM research_tasks WHERE id=?", e.Task).Scan(&used, &reserved); err != nil || used != 22887 || reserved != 0 || calls != 1 {
		t.Fatalf("review accounting used=%d reserved=%d calls=%d error=%v", used, reserved, calls, err)
	}
}
