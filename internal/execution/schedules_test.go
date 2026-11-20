package execution

import (
	"errors"
	"os"
	"testing"
	"time"

	"researchpilot/internal/service"
	taskservice "researchpilot/internal/task"
)

func TestResearchSchedulesIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	if os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
		t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	}
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	models := service.Models{Store: store}
	owner, foreign := newTestTask(t, db), newTestTask(t, db)
	first := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err := actions.CreateSchedule(ctx, foreign.Tenant, "no model", first.Format(time.RFC3339), 0, false); !errors.Is(err, service.ErrModelNotConfigured) {
		t.Fatalf("schedule without a personal model: %v", err)
	}
	version, err := models.Save(ctx, owner.Tenant, service.ModelInput{
		BaseURL: "https://api.deepseek.com", Model: "fixture", APIKey: "schedule-fixture-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	for goal, interval := range map[string]int{"once": 0, "daily": 1, "weekly": 7, "cancel": 0} {
		if err := actions.CreateSchedule(ctx, owner.Tenant, goal, first.Format(time.RFC3339), interval, goal == "daily"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := actions.ListSchedules(ctx, owner.Tenant)
	if err != nil || len(rows) != 4 {
		t.Fatalf("schedule list: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.Goal == "once" {
			if err := actions.SetScheduleSubscription(ctx, owner.Tenant, row.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		if row.Goal == "daily" {
			if err := actions.SetScheduleSubscription(ctx, foreign.Tenant, row.ID, false); err != nil {
				t.Fatal(err)
			}
		}
		if row.Goal != "cancel" {
			continue
		}
		if err := actions.CancelSchedule(ctx, foreign.Tenant, row.ID); !errors.Is(err, taskservice.ErrScheduleNotFound) {
			t.Fatalf("foreign cancel: %v", err)
		}
		if err := actions.CancelSchedule(ctx, owner.Tenant, row.ID); err != nil {
			t.Fatal(err)
		}
	}

	// Both workers observe an overdue daily schedule. One transaction advances it.
	now := first.Add(49 * time.Hour)
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- taskservice.RunDueSchedules(ctx, store, now) }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	rows, err = actions.ListSchedules(ctx, owner.Tenant)
	if err != nil || len(rows) != 4 {
		t.Fatalf("fired schedules: %+v %v", rows, err)
	}
	for _, row := range rows {
		switch row.Goal {
		case "cancel":
			if row.Status != "cancelled" || row.LastTaskID != nil {
				t.Fatalf("cancelled schedule was fired: %+v", row)
			}
			continue
		case "once":
			if row.Status != "completed" || row.NextRun != nil {
				t.Fatalf("one-time schedule did not complete: %+v", row)
			}
		case "daily", "weekly":
			next := first.AddDate(0, 0, 3)
			if row.Goal == "weekly" {
				next = first.AddDate(0, 0, 7)
			}
			if row.Status != "active" || row.NextRun == nil || *row.NextRun != next.Format(time.RFC3339) {
				t.Fatalf("missed runs did not advance to the next slot: %+v", row)
			}
		}
		if row.LastTaskID == nil {
			t.Fatalf("task was not recorded: %+v", row)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM research_tasks
			WHERE tenant_id=? AND goal=? AND model_version=? AND status='QUEUED'`, owner.Tenant, row.Goal, version).Scan(&count); err != nil || count != 1 {
			t.Fatalf("expected a single task: count=%d error=%v", count, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM task_outbox WHERE tenant_id=? AND task_id=?", owner.Tenant, *row.LastTaskID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("expected a single outbox entry: count=%d error=%v", count, err)
		}
	}
	if results, err := actions.ScheduleResults(ctx, owner.Tenant); err != nil || len(results) != 0 {
		t.Fatalf("active tasks must not appear in the result list: %+v %v", results, err)
	}
	var dailyID string
	for _, row := range rows {
		if row.LastTaskID == nil {
			continue
		}
		status := "SUCCEEDED"
		if row.Goal == "daily" {
			dailyID = *row.LastTaskID
			status = "FAILED"
			if err := actions.SetScheduleSubscription(ctx, owner.Tenant, row.ID, false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.ExecContext(ctx, "UPDATE research_tasks SET status=?,finished_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", status, owner.Tenant, *row.LastTaskID); err != nil {
			t.Fatal(err)
		}
	}
	if err := actions.MarkScheduleResultRead(ctx, foreign.Tenant, dailyID); err != nil {
		t.Fatal(err)
	}
	resultsList, err := actions.ScheduleResults(ctx, owner.Tenant)
	if err != nil || len(resultsList) != 2 {
		t.Fatalf("expected only subscribed terminal runs: %+v %v", resultsList, err)
	}
	var foundDaily bool
	for _, result := range resultsList {
		if !result.Unread {
			t.Fatal("new result was already read")
		}
		if result.TaskID == dailyID && result.Status == "FAILED" {
			foundDaily = true
		}
	}
	if !foundDaily {
		t.Fatal("failed scheduled run not found")
	}
	for range 2 {
		if err := actions.MarkScheduleResultRead(ctx, owner.Tenant, dailyID); err != nil {
			t.Fatal(err)
		}
	}
	resultsList, err = actions.ScheduleResults(ctx, owner.Tenant)
	if err != nil || len(resultsList) != 2 || resultsList[0].TaskID == dailyID || resultsList[1].TaskID != dailyID || resultsList[1].Unread {
		t.Fatalf("read state or unread-first ordering is incorrect: %+v %v", resultsList, err)
	}
	if results, err := actions.ScheduleResults(ctx, foreign.Tenant); err != nil || len(results) != 0 {
		t.Fatalf("foreign account received schedule results: %+v %v", results, err)
	}
	if err := models.Revoke(ctx, owner.Tenant); err != nil {
		t.Fatal(err)
	}
	if err := taskservice.RunDueSchedules(ctx, store, first.AddDate(0, 0, 7)); err != nil {
		t.Fatal(err)
	}
	rows, err = actions.ListSchedules(ctx, owner.Tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.IntervalDays > 0 && (row.Status != "blocked" || row.NextRun != nil) {
			t.Fatalf("revoked model did not block schedule: %+v", row)
		}
	}
}
