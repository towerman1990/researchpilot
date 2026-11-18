package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type evaluationCase struct{ ID, Goal string }

var evaluationCases = []evaluationCase{
	{"go-context", "根据 https://go.dev/blog/context 说明 Go Context 的取消传播和截止时间，给出两个常见误用。"},
	{"go-http", "根据 https://pkg.go.dev/net/http 说明 Go HTTP 服务的 ReadHeaderTimeout、IdleTimeout 与优雅关闭的区别。"},
	{"go-race", "根据 https://go.dev/doc/articles/race_detector 说明 Go 竞态检测的使用方式和覆盖局限。"},
	{"redis-persistence", "根据 https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/ 比较 Redis RDB 与 AOF 的数据恢复取舍。"},
	{"redis-transactions", "根据 https://redis.io/docs/latest/develop/using-commands/transactions/ 说明 Redis MULTI/EXEC 与 WATCH 的并发控制语义。"},
	{"mysql-isolation", "根据 https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html 比较 MySQL 8.4 InnoDB 的 REPEATABLE READ 与 READ COMMITTED。"},
	{"mysql-locking", "根据 https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html 说明 FOR UPDATE 和 SKIP LOCKED 适用的队列场景及限制。"},
	{"asynq-retries", "根据 https://github.com/hibiken/asynq 分析 Asynq 的任务重试与持久性能力，以及业务仍需幂等的原因。"},
	{"ssrf", "根据 https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html 总结服务端 URL 抓取的 SSRF 防护与重定向风险。"},
	{"sse", "根据 https://html.spec.whatwg.org/multipage/server-sent-events.html 说明 SSE 的事件 ID、Last-Event-ID 与重连机制。"},
}

type evaluationResult struct {
	ID                string     `json:"case_id"`
	Goal              string     `json:"goal"`
	TaskID            string     `json:"task_id"`
	Status            string     `json:"status"`
	Error             *string    `json:"error_code,omitempty"`
	Tokens            uint32     `json:"tokens_used"`
	Markdown          string     `json:"markdown,omitempty"`
	Sources           []evidence `json:"sources,omitempty"`
	CitationIntegrity bool       `json:"citation_integrity"`
	// Semantic acceptance requires an explicit review of claims against sources.
	HumanReview string `json:"human_review"`
}

func evaluate(ctx context.Context, db *sql.DB) error {
	if os.Getenv("EVAL_ALLOW_LIVE") != "1" {
		return errors.New("evaluation requires EVAL_ALLOW_LIVE=1; it incurs provider costs")
	}
	tenant := os.Getenv("EVAL_TENANT_ID")
	if tenant == "" {
		rows, err := db.QueryContext(ctx, "SELECT DISTINCT tenant_id FROM model_configs m WHERE revoked=false AND version=(SELECT MAX(version) FROM model_configs n WHERE n.tenant_id=m.tenant_id)")
		if err != nil {
			return err
		}
		var tenants []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			tenants = append(tenants, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(tenants) != 1 {
			return errors.New("configure exactly one active model in the UI, or select EVAL_TENANT_ID")
		}
		tenant = tenants[0]
	}
	var version uint32
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM model_configs WHERE tenant_id=?", tenant).Scan(&version); err != nil {
		return errors.New("evaluation model not configured")
	}
	config, err := loadModel(ctx, db, tenant, version)
	if err != nil {
		return errors.New("evaluation model unavailable")
	}
	cases := evaluationCases
	if raw := os.Getenv("EVAL_CASE_LIMIT"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > len(cases) {
			return errors.New("EVAL_CASE_LIMIT must be 1..10")
		}
		cases = cases[:limit]
	}
	if raw := os.Getenv("EVAL_CASE_IDS"); raw != "" {
		wanted := map[string]bool{}
		for _, id := range strings.Split(raw, ",") {
			wanted[strings.TrimSpace(id)] = true
		}
		var chosen []evaluationCase
		for _, c := range cases {
			if wanted[c.ID] {
				chosen = append(chosen, c)
				delete(wanted, c.ID)
			}
		}
		if len(wanted) > 0 || len(chosen) == 0 {
			return errors.New("unknown evaluation case ID")
		}
		cases = chosen
	}
	results := make([]evaluationResult, len(cases))
	for i, c := range cases {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		d := delivery{Tenant: tenant, Task: identifier(16), Generation: 1}
		_, err = tx.ExecContext(ctx, "INSERT INTO research_tasks(id,tenant_id,goal,status,deadline_at,event_seq,runtime_version,model_version) VALUES(?,?,?,'QUEUED',UTC_TIMESTAMP(6)+INTERVAL 5 MINUTE,1,'m2.2',?)", d.Task, tenant, c.Goal, version)
		if err == nil {
			err = appendEvent(ctx, tx, tenant, d.Task, 1, "task_status", map[string]string{"status": "QUEUED"})
		}
		if err == nil {
			err = addOutbox(ctx, tx, tenant, d.Task, 1)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		results[i] = evaluationResult{ID: c.ID, Goal: c.Goal, TaskID: d.Task, HumanReview: "PENDING"}
	}
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	api := apiServer{db: db}
	for {
		complete := true
		for i := range results {
			r := &results[i]
			if terminal(r.Status) {
				continue
			}
			snapshot, err := api.readTask(ctx, tenant, r.TaskID)
			if err != nil {
				return err
			}
			r.Status = snapshot.Status
			r.Error = snapshot.Error
			r.Tokens = snapshot.TokensUsed
			if !terminal(r.Status) {
				complete = false
				continue
			}
			if r.Status == "SUCCEEDED" || r.Status == "PARTIAL" {
				if err = db.QueryRowContext(ctx, "SELECT markdown FROM research_reports WHERE tenant_id=? AND task_id=?", tenant, r.TaskID).Scan(&r.Markdown); err != nil {
					return err
				}
				rows, err := db.QueryContext(ctx, "SELECT s.source_id,s.title,s.canonical_url,s.excerpt FROM report_citations c JOIN research_sources s ON s.tenant_id=c.tenant_id AND s.task_id=c.task_id AND s.source_id=c.source_id WHERE c.tenant_id=? AND c.task_id=? ORDER BY c.ordinal", tenant, r.TaskID)
				if err != nil {
					return err
				}
				for rows.Next() {
					var s evidence
					if err = rows.Scan(&s.ID, &s.Title, &s.URL, &s.Excerpt); err != nil {
						rows.Close()
						return err
					}
					r.Sources = append(r.Sources, s)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return err
				}
				var count int
				if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM report_citations WHERE tenant_id=? AND task_id=?", tenant, r.TaskID).Scan(&count); err != nil {
					return err
				}
				r.CitationIntegrity = count > 0 && count == len(r.Sources)
			}
		}
		if complete {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-poll.C:
		}
	}
	succeeded := 0
	for _, r := range results {
		if r.Status == "SUCCEEDED" && r.CitationIntegrity {
			succeeded++
		}
	}
	report := map[string]any{"schema_version": 1, "evaluated_at": time.Now().UTC(), "model": config.Model, "model_version": version, "cases": results, "automated_successes": succeeded, "required_human_accepted": 8, "quality_accepted": false, "smoke_only": len(cases) < len(evaluationCases), "note": "Automated success does not establish semantic evidence support. Review every report before setting acceptance."}
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	if succeeded < min(8, len(cases)) {
		return fmt.Errorf("only %d of %d reports completed; this run requires %d; inspect evaluation output", succeeded, len(cases), min(8, len(cases)))
	}
	return nil
}
