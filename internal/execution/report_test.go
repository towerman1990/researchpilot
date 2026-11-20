package execution

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
	taskservice "researchpilot/internal/task"
)

func TestCitationReviewRejectsIncompleteDecisions(t *testing.T) {
	var draft reportDraft
	if err := json.Unmarshal([]byte(`{"findings":[{"text":"one","sources":["one"]},{"text":"two","sources":["two"]}]}`), &draft); err != nil {
		t.Fatal(err)
	}
	for _, decision := range []string{`{}`, `{"supported":[true]}`, `{"supported":[true,null]}`, `{"supported":[true,false,true]}`} {
		if _, _, err := applyCitationReview(draft, decision); !errors.Is(err, errCitationReviewInvalid) {
			t.Fatalf("unexpected decision for %s: %v", decision, err)
		}
	}
	if _, _, err := applyCitationReview(draft, `{"supported":[false,false]}`); !errors.Is(err, errCitationReviewUnsupported) {
		t.Fatalf("unsubstantiated draft returned %v", err)
	}
}

func TestReportCitationsAreTaskBound(t *testing.T) {
	var draft reportDraft
	draft.Findings = append(draft.Findings, struct {
		Text    string   `json:"text"`
		Sources []string `json:"sources"`
	}{Text: "claim", Sources: []string{"invented"}})
	if _, _, err := renderReport(draft, []evidence{{ID: "real", Title: "title", URL: "https://example.com"}}); err == nil {
		t.Fatal("invented reference accepted")
	}
}

func TestReportFeedbackIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	e, _ := claimTest(t, db)
	tasks := taskservice.Tasks{Store: executionStore(t, db)}
	input := model.ReportFeedback{Rating: "helpful", Comment: " 有参考价值 "}
	if _, err := tasks.SaveReportFeedback(ctx, e.Tenant, e.Task, input); !errors.Is(err, taskservice.ErrReportNotReady) {
		t.Fatalf("running task accepted feedback: %v", err)
	}
	if err := e.mutate(ctx, func(tx *repository.ExecutionTx, locked taskRow) error {
		return finishReport(ctx, tx, e.delivery, locked, "fixture", "demo", nil)
	}); err != nil {
		t.Fatal(err)
	}
	if value, err := tasks.ReportFeedback(ctx, e.Tenant, e.Task); err != nil || value != (model.ReportFeedback{}) {
		t.Fatalf("unexpected initial feedback: %+v %v", value, err)
	}
	if value, err := tasks.SaveReportFeedback(ctx, e.Tenant, e.Task, input); err != nil || value.Comment != "有参考价值" {
		t.Fatalf("save: %+v %v", value, err)
	}
	update := model.ReportFeedback{Rating: "not_helpful", Comment: strings.Repeat("📝", 500)}
	if _, err := tasks.SaveReportFeedback(ctx, e.Tenant, e.Task, update); err != nil {
		t.Fatal(err)
	}
	invalid := model.ReportFeedback{Rating: "helpful", Comment: update.Comment + "多"}
	if _, err := tasks.SaveReportFeedback(ctx, e.Tenant, e.Task, invalid); !errors.Is(err, taskservice.ErrInvalidReportFeedback) {
		t.Fatalf("oversized comment accepted: %v", err)
	}
	foreign := newTestTask(t, db)
	if _, err := tasks.SaveReportFeedback(ctx, foreign.Tenant, e.Task, input); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign account accepted: %v", err)
	}
	if value, err := tasks.ReportFeedback(ctx, e.Tenant, e.Task); err != nil || value != update {
		t.Fatalf("updated feedback was changed: rating=%q commentBytes=%d error=%v", value.Rating, len(value.Comment), err)
	}
}

func TestReportEvidenceIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	e, _ := claimTest(t, db)
	tasks := taskservice.Tasks{Store: executionStore(t, db)}
	if _, err := tasks.ReportEvidence(ctx, e.Tenant, e.Task); !errors.Is(err, taskservice.ErrReportNotReady) {
		t.Fatalf("running task returned %v", err)
	}
	sources, err := e.saveSources(ctx, []evidence{
		{Title: "First", URL: "https://example.com/one", Excerpt: "One"},
		{Title: "Second", URL: "https://example.com/two", Excerpt: "Two"},
		{Title: "Unused", URL: "https://example.com/three", Excerpt: "Three"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.mutate(ctx, func(tx *repository.ExecutionTx, locked taskRow) error {
		return finishReport(ctx, tx, e.delivery, locked, "fixture", "research", []evidence{sources[1], sources[0]})
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := tasks.ReportEvidence(ctx, e.Tenant, e.Task)
	if err != nil || summary.Kind != "research" || summary.ArchivedSources != 3 || len(summary.Citations) != 2 ||
		summary.Citations[0].ID != sources[1].ID || summary.Citations[0].Ordinal != 1 ||
		summary.Citations[1].ID != sources[0].ID || summary.Citations[1].Ordinal != 2 {
		t.Fatalf("unexpected evidence summary: %+v, %v", summary, err)
	}
	foreign := newTestTask(t, db)
	if _, err := tasks.ReportEvidence(ctx, foreign.Tenant, e.Task); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign report returned %v", err)
	}
}

func TestReportCitationFailureRollsBackIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	e, state := claimTest(t, db)
	sources, err := e.saveSources(ctx, []evidence{{Title: "Fixture", URL: "https://example.com", Excerpt: "Evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	citations := append(sources, evidence{ID: identity.New(16)})
	err = e.mutate(ctx, func(tx *repository.ExecutionTx, locked taskRow) error {
		return finishReport(ctx, tx, e.delivery, locked, "fixture report", "research", citations)
	})
	var dbErr *mysql.MySQLError
	if !errors.As(err, &dbErr) || dbErr.Number != 1452 {
		t.Fatalf("expected citation foreign key failure, got %v", err)
	}
	if _, err := e.store.ReadReport(ctx, e.Tenant, e.Task); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected report insert to roll back, got %v", err)
	}
	task, err := e.store.ReadTask(ctx, e.Tenant, e.Task)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "RUNNING" || task.Seq != state.seq {
		t.Fatalf("task changed after rollback: status=%s seq=%d", task.Status, task.Seq)
	}
}
