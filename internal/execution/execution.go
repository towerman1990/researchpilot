// Package execution runs and recovers durable research tasks.
package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hibiken/asynq"

	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
	taskservice "researchpilot/internal/task"
	toolcontract "researchpilot/internal/tools"
)

var errLeaseLost = ErrLeaseLost
var errBudget = ErrBudget

type execution struct {
	Runner
	delivery
	epoch uint32
}

// mutate serializes every execution write with cancellation, takeover and finalization.
func (e execution) mutate(ctx context.Context, f func(*repository.ExecutionTx, taskRow) error) error {
	return e.store.TransactExecution(ctx, func(tx *repository.ExecutionTx) error {
		t, err := lockTask(ctx, tx, e.delivery)
		if err != nil {
			return err
		}
		if err := CheckLease(Lease{Status: t.status, Epoch: t.epoch, Valid: t.validLease, PastDeadline: t.pastDeadline}, e.epoch); err != nil {
			return err
		}
		return f(tx, t)
	})
}

func eventUpdate(ctx context.Context, tx *repository.ExecutionTx, d delivery, t taskRow, kind string, payload any) error {
	if err := tx.SetEventSequence(ctx, d.Tenant, d.Task, t.seq+1); err != nil {
		return err
	}
	return appendEvent(ctx, tx, d.Tenant, d.Task, t.seq+1, kind, payload)
}

func (e execution) keepAlive(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}) {
	RenewLease(ctx, cancel, done, func(ctx context.Context) (int64, error) {
		return e.store.RenewExecutionLease(ctx, e.Tenant, e.Task, e.epoch)
	})
}

const runTask = taskservice.RunTaskName

type delivery = model.Delivery
type evidence = model.Source

// These dependencies keep the local stub replaceable without changing the worker's persistence.
type ReportProvider interface {
	Report(context.Context, string, string) (string, error)
}

type ResearchTool interface {
	Collect(context.Context, string) (string, error)
}

type stubTool struct{}
type stubProvider struct{}

func (stubTool) Collect(ctx context.Context, _ string) (string, error) {
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "本地固定工具结果，未访问外部资料。", nil
	}
}

func (stubProvider) Report(_ context.Context, goal, observation string) (string, error) {
	return fmt.Sprintf("# M1 演示报告\n\n研究目标：%s\n\n## 工具结果\n\n%s\n\n## 结果说明\n\n本报告用于验证任务流转。没有外部证据或引用。", goal, observation), nil
}

type Runner struct {
	store     *repository.Store
	model     ReportProvider
	tool      ResearchTool
	modelHTTP *http.Client

	registry *toolcontract.Registry[[]evidence]
}

func NewRunner(store *repository.Store, provider ReportProvider, tool ResearchTool, modelHTTP *http.Client, registry *toolcontract.Registry[[]evidence]) Runner {
	return Runner{store: store, model: provider, tool: tool, modelHTTP: modelHTTP, registry: registry}
}
func NewStubProvider() ReportProvider { return stubProvider{} }
func NewStubTool() ResearchTool       { return stubTool{} }

type taskRow struct {
	status, goal             string
	generation, epoch        uint32
	seq                      uint64
	pastDeadline, validLease bool
	retries                  uint16
	remaining                time.Duration
	modelVersion             uint32
}

func lockTask(ctx context.Context, tx *repository.ExecutionTx, d delivery) (t taskRow, err error) {
	row, err := tx.LockExecutionTask(ctx, d.Tenant, d.Task)
	if err != nil {
		return t, err
	}
	return taskRow{status: row.Status, goal: row.Goal, generation: row.Generation, epoch: row.LeaseEpoch, seq: row.EventSeq, pastDeadline: row.PastDeadline, validLease: row.ValidLease, retries: row.RetryCount, remaining: time.Duration(row.RemainingSeconds * float64(time.Second)), modelVersion: row.ModelVersion}, nil
}

func (r Runner) claim(ctx context.Context, d delivery) (t taskRow, claimed bool, err error) {
	err = r.store.TransactExecution(ctx, func(tx *repository.ExecutionTx) error {
		var err error
		t, err = lockTask(ctx, tx, d)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if t.status != "QUEUED" || t.generation != d.Generation || t.pastDeadline {
			return nil
		}
		t.epoch++
		t.seq++
		if err := tx.ClaimExecutionTask(ctx, d.Tenant, d.Task, t.epoch, t.seq); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, d.Tenant, d.Task, t.seq, "task_status", map[string]string{"status": "RUNNING", "phase": "planning"}); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return t, claimed && err == nil, err
}

func (r Runner) Execute(ctx context.Context, task *asynq.Task) error {
	var d delivery
	if err := json.Unmarshal(task.Payload(), &d); err != nil || len(d.Task) != 32 || len(d.Tenant) != 32 || d.Generation == 0 {
		return asynq.SkipRetry
	}
	t, claimed, err := r.claim(ctx, d)
	if err != nil || !claimed {
		return err
	}

	return r.executeDurable(ctx, d, t)
}

func (r Runner) executeDurable(ctx context.Context, d delivery, t taskRow) error {
	execCtx, cancel := context.WithTimeout(ctx, t.remaining)
	e := execution{Runner: r, delivery: d, epoch: t.epoch}
	done := make(chan struct{})
	go e.keepAlive(execCtx, cancel, done)
	defer func() { cancel(); <-done }()
	if t.modelVersion > 0 {
		err := e.executeResearch(execCtx, t)
		if err == nil {
			return nil
		}
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()
		return e.failed(cleanup, err)
	}
	observation, err := e.checkpoint(execCtx, "collect", func(ctx context.Context) (string, error) { return r.tool.Collect(ctx, t.goal) })
	if err == nil {
		var report string
		report, err = e.checkpoint(execCtx, "report", func(ctx context.Context) (string, error) {
			// Stub output is deterministic and incurs zero external token usage.
			id, err := e.reserve(ctx, 1)
			if err != nil {
				return "", err
			}
			value, err := r.model.Report(ctx, t.goal, observation)
			if err != nil {
				return "", err
			}
			if err = e.settle(ctx, id, 0, false); err != nil {
				return "", err
			}
			return value, nil
		})
		if err == nil {
			err = e.mutate(execCtx, func(tx *repository.ExecutionTx, current taskRow) error {
				return finishReport(execCtx, tx, d, current, report, "demo", nil)
			})
		}
	}
	if err == nil {
		return nil
	}
	cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelCleanup()
	if failureErr := e.failed(cleanup, err); failureErr != nil {
		return fmt.Errorf("settle execution failure: %w", failureErr)
	}
	return nil
}

func permanentFailure(err error) bool {
	var exhausted *modelAttemptsExhausted
	if errors.As(err, &exhausted) {
		return true
	}
	var upstream *modelError
	if errors.As(err, &upstream) {
		return !upstream.Retryable()
	}
	return knownFailureCode(err) != ""
}
func safeFailureCode(err error) string {
	var exhausted *modelAttemptsExhausted
	if errors.As(err, &exhausted) {
		return exhausted.Cause.Error()
	}
	var upstream *modelError
	if errors.As(err, &upstream) {
		return upstream.Error()
	}
	if code := knownFailureCode(err); code != "" {
		return code
	}
	return "execution_failed"
}

func knownFailureCode(err error) string {
	for ; err != nil; err = errors.Unwrap(err) {
		switch err.Error() {
		case "insufficient_evidence", "model_output_truncated", "model_dns_policy_denied", "model_dns_resolution_failed", "invalid_outbound_dns_config", "model_tls_failed", "model_retry_exhausted", "model_configuration_revoked", "invalid_model_config", "invalid_report_json", "invalid_source_id", "no_evidence":
			return err.Error()
		}
	}
	return ""
}

var ErrLeaseLost = errors.New("execution lease lost")

// Lease describes the task state read under the database row lock.
type Lease struct {
	Status       string
	Epoch        uint32
	Valid        bool
	PastDeadline bool
}

// CheckLease prevents a stale worker from changing a task.
func CheckLease(lease Lease, epoch uint32) error {
	if lease.Status != "RUNNING" || lease.Epoch != epoch || !lease.Valid || lease.PastDeadline {
		return ErrLeaseLost
	}
	return nil
}

// RenewLease stops the execution when its persisted lease can no longer be renewed.
func RenewLease(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}, renew func(context.Context) (int64, error)) {
	defer close(done)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := renew(ctx)
			if err != nil || count != 1 {
				cancel()
				return
			}
		}
	}
}

func appendEvent(ctx context.Context, tx *repository.ExecutionTx, tenant, id string, seq uint64, kind string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return tx.InsertTaskEvent(ctx, tenant, id, seq, kind, string(raw))
}
