package main

import (
	"context"
	"database/sql"
)

// Only terminal tasks older than the retention window are pruned. Updating the
// replay floor and deleting events share the same task lock and transaction.
func pruneEvents(ctx context.Context, db *sql.DB) error {
	days := quota("EVENT_RETENTION_DAYS", 30)
	rows, err := db.QueryContext(ctx, "SELECT tenant_id,id FROM research_tasks WHERE status IN ('SUCCEEDED','PARTIAL','FAILED','CANCELLED') AND finished_at<TIMESTAMPADD(DAY,-?,UTC_TIMESTAMP(6)) AND min_event_seq<=event_seq LIMIT 100", days)
	if err != nil {
		return err
	}
	var tasks []delivery
	for rows.Next() {
		var d delivery
		if err = rows.Scan(&d.Tenant, &d.Task); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range tasks {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		t, err := lockTask(ctx, tx, d)
		if err == nil && !terminal(t.status) {
			tx.Rollback()
			continue
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE research_tasks SET min_event_seq=event_seq+1 WHERE tenant_id=? AND id=?", d.Tenant, d.Task)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM task_events WHERE tenant_id=? AND task_id=?", d.Tenant, d.Task)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
