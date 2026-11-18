package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// A partial report quotes archived source excerpts. It makes no new model
// claims and incurs no additional tokens when the task budget is exhausted.
func publishPartial(ctx context.Context, tx *sql.Tx, d delivery, t taskRow, reason string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT source_id,title,canonical_url,excerpt FROM research_sources WHERE tenant_id=? AND task_id=? ORDER BY retrieved_at,source_id LIMIT 8", d.Tenant, d.Task)
	if err != nil {
		return false, err
	}
	var sources []evidence
	for rows.Next() {
		var s evidence
		if err = rows.Scan(&s.ID, &s.Title, &s.URL, &s.Excerpt); err != nil {
			rows.Close()
			return false, err
		}
		if s.Excerpt != "" {
			sources = append(sources, s)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(sources) == 0 {
		return false, err
	}
	draft := reportDraft{Limitations: []string{"研究未完成，以下仅为已归档资料摘录，尚未形成完整研究结论。", fmt.Sprintf("结束原因：%s。", reason)}}
	for _, s := range sources {
		draft.Findings = append(draft.Findings, struct {
			Text    string   `json:"text"`
			Sources []string `json:"sources"`
		}{Text: "资料摘录：" + clip(s.Excerpt, 1000), Sources: []string{s.ID}})
	}
	body, citations, err := renderReport(draft, sources)
	if err != nil {
		return false, err
	}
	body = strings.Replace(body, "# 研究报告", "# 部分研究成果", 1)
	if _, err = tx.ExecContext(ctx, "INSERT INTO research_reports(tenant_id,task_id,markdown,kind) VALUES(?,?,?,'partial')", d.Tenant, d.Task, body); err != nil {
		return false, err
	}
	for i, s := range citations {
		if _, err = tx.ExecContext(ctx, "INSERT INTO report_citations VALUES(?,?,?,?)", d.Tenant, d.Task, i+1, s.ID); err != nil {
			return false, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE research_tasks SET status='PARTIAL' WHERE tenant_id=? AND id=?", d.Tenant, d.Task); err != nil {
		return false, err
	}
	return true, eventUpdate(ctx, tx, d, t, "report_ready", map[string]string{"status": "PARTIAL", "error_code": reason})
}
