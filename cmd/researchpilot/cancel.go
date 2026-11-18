package main

import (
	"database/sql"
	"errors"
	"net/http"
)

func (a *apiServer) cancelTask(w http.ResponseWriter, r *http.Request, tenant string) {
	var body struct{}
	if !parse(w, r, &body) {
		return
	}
	ctx := r.Context()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()
	d := delivery{Tenant: tenant, Task: r.PathValue("id")}
	t, err := lockTask(ctx, tx, d)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "task_not_found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if terminal(t.status) && t.status != "CANCELLED" {
		problem(w, 409, "task_already_finished")
		return
	}
	if t.status != "CANCELLED" && t.status != "CANCELLING" {
		if _, err = tx.ExecContext(ctx, "UPDATE research_tasks SET cancel_requested_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", tenant, d.Task); err != nil {
			internal(w, err)
			return
		}
		if t.status == "RUNNING" {
			_, err = tx.ExecContext(ctx, "UPDATE research_tasks SET status='CANCELLING' WHERE tenant_id=? AND id=?", tenant, d.Task)
			if err == nil {
				err = eventUpdate(ctx, tx, d, t, "task_status", map[string]string{"status": "CANCELLING"})
			}
		} else {
			err = finishTask(ctx, tx, d, t, "CANCELLED", "")
		}
		if err != nil {
			internal(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		internal(w, err)
		return
	}
	s, err := a.readTask(ctx, tenant, d.Task)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, 202, s)
}
