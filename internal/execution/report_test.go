package execution

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository"
)

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
