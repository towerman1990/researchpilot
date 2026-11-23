// Package evaluation runs the technical research acceptance suite.
package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	taskservice "researchpilot/internal/task"
	"strconv"
	"strings"
	"time"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/repository/model"
	"researchpilot/internal/service"
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
	ID                string         `json:"case_id"`
	Goal              string         `json:"goal"`
	TaskID            string         `json:"task_id"`
	Status            string         `json:"status"`
	Error             *string        `json:"error_code,omitempty"`
	Tokens            uint32         `json:"tokens_used"`
	Markdown          string         `json:"markdown,omitempty"`
	Sources           []model.Source `json:"sources,omitempty"`
	CitationIntegrity bool           `json:"citation_integrity"`
	// Semantic acceptance requires an explicit review of claims against sources.
	HumanReview string `json:"human_review"`
}

type Store interface {
	EvaluationTenants(context.Context) ([]string, error)
	ActiveModelVersion(context.Context, string) (uint32, error)
	Credential(context.Context, string, uint32) (model.Credential, error)
	EvaluationCreate(context.Context, string, string, string, uint32) error
	ReadTask(context.Context, string, string) (model.TaskSnapshot, error)
	ReadReport(context.Context, string, string) (string, error)
	EvaluationSources(context.Context, string, string) ([]model.Source, error)
	EvaluationCitationCount(context.Context, string, string) (int, error)
}
type Evaluator struct{ store Store }

func New(store Store) *Evaluator { return &Evaluator{store: store} }
func (e *Evaluator) Run(ctx context.Context) error {
	return e.run(ctx, os.Stdout)
}

func (e *Evaluator) run(ctx context.Context, output io.Writer) error {
	store := e.store
	if os.Getenv("EVAL_ALLOW_LIVE") != "1" {
		return errors.New("evaluation requires EVAL_ALLOW_LIVE=1; it incurs provider costs")
	}
	tenant := os.Getenv("EVAL_TENANT_ID")
	if tenant == "" {
		tenants, err := store.EvaluationTenants(ctx)
		if err != nil {
			return err
		}
		if len(tenants) != 1 {
			return errors.New("configure exactly one active model in the UI, or select EVAL_TENANT_ID")
		}
		tenant = tenants[0]
	}
	version, err := store.ActiveModelVersion(ctx, tenant)
	if err != nil || version == 0 {
		return errors.New("evaluation model not configured")
	}
	config, err := service.LoadModel(ctx, store, tenant, version)
	if err != nil {
		return errors.New("evaluation model unavailable")
	}
	sourceMode := os.Getenv("EVAL_SOURCE_MODE")
	if sourceMode == "" {
		sourceMode = "seeded"
	}
	cases, err := casesForSourceMode(sourceMode)
	if err != nil {
		return err
	}
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
	timeout := 8 * time.Minute
	if raw := os.Getenv("EVAL_TIMEOUT"); raw != "" {
		timeout, err = time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return errors.New("EVAL_TIMEOUT must be a positive duration, such as 8m")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results := make([]evaluationResult, len(cases))
	for i, c := range cases {
		results[i] = evaluationResult{ID: c.ID, Goal: c.Goal, Status: "NOT_SUBMITTED", HumanReview: "PENDING"}
	}
	var runErr error
	for i, c := range cases {
		id := identity.New(16)
		if err := store.EvaluationCreate(ctx, tenant, id, c.Goal, version); err != nil {
			runErr = fmt.Errorf("submit case %s: %w", c.ID, err)
			break
		}
		results[i].TaskID, results[i].Status = id, "QUEUED"
	}
	if runErr == nil {
		runErr = waitForResults(ctx, store, tenant, results)
	}
	status := "completed"
	switch {
	case errors.Is(runErr, context.DeadlineExceeded):
		status = "timed_out"
	case errors.Is(runErr, context.Canceled):
		status = "cancelled"
	case runErr != nil:
		status = "interrupted"
	}
	unfinished := make([]string, 0)
	for _, r := range results {
		if r.TaskID != "" && !taskservice.Terminal(r.Status) {
			unfinished = append(unfinished, r.TaskID)
		}
	}
	succeeded := 0
	for _, r := range results {
		if r.Status == "SUCCEEDED" && r.CitationIntegrity {
			succeeded++
		}
	}
	report := map[string]any{"schema_version": 1, "source_mode": sourceMode, "status": status, "unfinished_task_ids": unfinished, "evaluated_at": time.Now().UTC(), "model": config.Model, "model_version": version, "cases": results, "automated_successes": succeeded, "required_human_accepted": 8, "quality_accepted": false, "smoke_only": len(cases) < len(evaluationCases), "note": "Automated success does not establish semantic evidence support. Review every report before setting acceptance."}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return errors.Join(runErr, err)
	}
	if runErr != nil {
		return fmt.Errorf("evaluation %s: %w", status, runErr)
	}
	if succeeded < min(8, len(cases)) {
		return fmt.Errorf("only %d of %d reports completed; this run requires %d; inspect evaluation output", succeeded, len(cases), min(8, len(cases)))
	}
	return nil
}

func casesForSourceMode(mode string) ([]evaluationCase, error) {
	switch mode {
	case "seeded":
		return evaluationCases, nil
	case "unseeded":
		cases := make([]evaluationCase, len(evaluationCases))
		for i, item := range evaluationCases {
			rest, hasPrefix := strings.CutPrefix(item.Goal, "根据 https://")
			_, question, hasQuestion := strings.Cut(rest, " ")
			if !hasPrefix || !hasQuestion || question == "" || strings.Contains(question, "://") {
				return nil, fmt.Errorf("cannot remove the source URL from case %s", item.ID)
			}
			// This case gets its database and version from the supplied manual URL.
			if item.ID == "mysql-locking" {
				question = "在 MySQL 8.4 InnoDB 中，" + question
			}
			cases[i] = evaluationCase{ID: item.ID, Goal: question}
		}
		return cases, nil
	default:
		return nil, errors.New("EVAL_SOURCE_MODE must be seeded or unseeded")
	}
}

func waitForResults(ctx context.Context, store Store, tenant string, results []evaluationResult) error {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		complete := true
		for i := range results {
			r := &results[i]
			if taskservice.Terminal(r.Status) {
				continue
			}
			snapshot, err := store.ReadTask(ctx, tenant, r.TaskID)
			if err != nil {
				return err
			}
			r.Status = snapshot.Status
			r.Error = snapshot.Error
			r.Tokens = snapshot.TokensUsed
			if !taskservice.Terminal(r.Status) {
				complete = false
				continue
			}
			if r.Status == "SUCCEEDED" || r.Status == "PARTIAL" {
				r.Markdown, err = store.ReadReport(ctx, tenant, r.TaskID)
				if err != nil {
					return err
				}
				r.Sources, err = store.EvaluationSources(ctx, tenant, r.TaskID)
				if err != nil {
					return err
				}
				count, err := store.EvaluationCitationCount(ctx, tenant, r.TaskID)
				if err != nil {
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
	return nil
}
