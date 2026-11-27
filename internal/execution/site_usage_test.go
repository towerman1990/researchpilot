package execution

import (
	"errors"
	"testing"
	"time"

	"researchpilot/internal/repository"
	taskservice "researchpilot/internal/task"
)

func TestSiteUsageIntegration(t *testing.T) {
	db := integrationDB(t)
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	now := time.Now()
	before, err := store.SiteUsage(t.Context(), now)
	if err != nil {
		t.Fatal(err)
	}
	account, guestA, guestB := newTestTask(t, db), newTestTask(t, db), newTestTask(t, db)
	var ids []string
	for i, input := range []struct {
		tenant, key string
		options     taskservice.SubmitOptions
	}{
		{account.Tenant, "site-usage-account-one", taskservice.SubmitOptions{}},
		{account.Tenant, "site-usage-account-two", taskservice.SubmitOptions{}},
		{guestA.Tenant, "site-usage-guest-one", taskservice.SubmitOptions{GuestAddress: account.Task}},
		{guestB.Tenant, "site-usage-guest-two", taskservice.SubmitOptions{GuestAddress: account.Task}},
	} {
		created, err := actions.Submit(t.Context(), input.tenant, input.key, "Statistics fixture", "", input.options)
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		replayed, err := actions.Submit(t.Context(), input.tenant, input.key, "Statistics fixture", "", input.options)
		if err != nil || replayed.ID != created.ID {
			t.Fatalf("replay %d: %+v %v", i, replayed, err)
		}
		ids = append(ids, created.ID)
	}
	stats, err := store.SiteUsage(t.Context(), now)
	if err != nil || stats.TotalUses != before.TotalUses+4 || stats.TotalUsers != before.TotalUsers+2 ||
		stats.TodayUses != before.TodayUses+4 || stats.TodayUsers != before.TodayUsers+2 {
		t.Fatalf("account/IP/retry counts: before=%+v after=%+v err=%v", before, stats, err)
	}
	rollback := errors.New("fixture rollback")
	err = store.Write(t.Context(), account.Tenant, true, func(tx repository.WriteTx) error {
		if err := tx.RecordSiteResearchUse(account.Task, account.Tenant, ""); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	afterRollback, err := store.SiteUsage(t.Context(), now)
	if err != nil || afterRollback != stats {
		t.Fatalf("rollback leaked statistics: %+v %v", afterRollback, err)
	}
	for _, id := range ids {
		if _, err := db.Exec("UPDATE site_research_usage SET usage_day=DATE_SUB(?,INTERVAL 1 DAY) WHERE task_id=?", stats.Day, id); err != nil {
			t.Fatal(err)
		}
	}
	today, err := store.SiteUsage(t.Context(), now)
	if err != nil || today.TotalUses != stats.TotalUses || today.TotalUsers != stats.TotalUsers ||
		today.TodayUses != before.TodayUses || today.TodayUsers != before.TodayUsers {
		t.Fatalf("daily totals included historical events: %+v %v", today, err)
	}
}
