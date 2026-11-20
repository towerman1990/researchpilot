package execution

import (
	"testing"
	"time"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository/model"
	taskservice "researchpilot/internal/task"
)

func TestResearchEvaluationOverviewIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	owner, foreign := newTestTask(t, db), newTestTask(t, db)
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)

	for _, fixture := range []struct {
		tenant, goal, status, report, rating string
		offset                               time.Duration
		version                              any
		cited                                bool
	}{
		{owner.Tenant, "complete", "SUCCEEDED", "research", "helpful", 0, 1, true},
		{owner.Tenant, "low rating", "SUCCEEDED", "research", "not_helpful", -time.Minute, 1, true},
		{owner.Tenant, "partial", "PARTIAL", "partial", "", -2 * time.Minute, 1, false},
		{owner.Tenant, "failed", "FAILED", "", "", -3 * time.Minute, 1, false},
		{owner.Tenant, "cancelled", "CANCELLED", "", "", -4 * time.Minute, 1, false},
		{owner.Tenant, "demo", "SUCCEEDED", "demo", "", -5 * time.Minute, nil, false},
		{owner.Tenant, "older", "FAILED", "", "", -10 * 24 * time.Hour, 1, false},
		{foreign.Tenant, "foreign", "FAILED", "", "", 0, 1, false},
	} {
		id := identity.New(16)
		if _, err := db.ExecContext(ctx, `INSERT INTO research_tasks(id,tenant_id,goal,status,event_seq,deadline_at,finished_at,model_version)
			VALUES(?,?,?,?,1,UTC_TIMESTAMP(6),?,?)`, id, fixture.tenant, fixture.goal, fixture.status, now.Add(fixture.offset), fixture.version); err != nil {
			t.Fatal(err)
		}
		if fixture.report == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO research_reports (tenant_id,task_id,markdown,kind) VALUES(?,?,?,?)", fixture.tenant, id, "fixture", fixture.report); err != nil {
			t.Fatal(err)
		}
		if fixture.rating != "" {
			if _, err := db.ExecContext(ctx, "INSERT INTO report_feedback(tenant_id,task_id,rating) VALUES(?,?,?)", fixture.tenant, id, fixture.rating); err != nil {
				t.Fatal(err)
			}
		}
		if fixture.cited {
			sourceID := identity.New(16)
			if _, err := db.ExecContext(ctx, `INSERT INTO research_sources(tenant_id,task_id,source_id,title,canonical_url,excerpt,fingerprint)
				VALUES(?,?,?,'source','https://example.com/','excerpt',UNHEX(SHA2(?,256)))`, fixture.tenant, id, sourceID, sourceID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "INSERT INTO report_citations(tenant_id,task_id,ordinal,source_id) VALUES(?,?,1,?)", fixture.tenant, id, sourceID); err != nil {
				t.Fatal(err)
			}
		}
	}
	tasks := taskservice.Tasks{Store: executionStore(t, db)}
	week, err := tasks.Evaluation(ctx, owner.Tenant, "7", now)
	want := model.EvaluationSummary{FinishedTasks: 5, Succeeded: 2, Partial: 1, Failed: 1, Cancelled: 1, Reports: 3, CitedReports: 2, Helpful: 1, NotHelpful: 1}
	if err != nil || week.Summary != want || len(week.ReviewQueue) != 3 ||
		week.ReviewQueue[0].Goal != "low rating" || week.ReviewQueue[1].Goal != "partial" || week.ReviewQueue[2].Goal != "failed" {
		t.Fatalf("unexpected weekly evaluation: %+v %v", week, err)
	}
	month, err := tasks.Evaluation(ctx, owner.Tenant, "30", now)
	if err != nil || month.Summary.FinishedTasks != 6 || month.Summary.Failed != 2 || len(month.ReviewQueue) != 4 {
		t.Fatalf("unexpected monthly evaluation: %+v %v", month, err)
	}
}
