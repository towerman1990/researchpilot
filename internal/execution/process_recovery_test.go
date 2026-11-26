package execution

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestProcessRecoveryIntegration(t *testing.T) {
	db := integrationDB(t)
	runner := Runner{store: executionStore(t, db), tool: stubTool{}, model: stubProvider{}}
	if phase := os.Getenv("RP_TEST_RECOVERY_PHASE"); phase != "" {
		generation, err := strconv.ParseUint(os.Getenv("RP_TEST_RECOVERY_GENERATION"), 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		d := delivery{Tenant: os.Getenv("RP_TEST_RECOVERY_TENANT"), Task: os.Getenv("RP_TEST_RECOVERY_TASK"), Generation: uint32(generation)}
		state, ok, err := runner.claim(t.Context(), d)
		if err != nil || !ok {
			t.Fatalf("worker claim: %v %v", ok, err)
		}
		if phase == "resume" {
			if err := runner.executeDurable(t.Context(), d, state); err != nil {
				t.Fatal(err)
			}
			return
		}
		e := execution{Runner: runner, delivery: d, epoch: state.epoch}
		if _, err := e.checkpoint(t.Context(), "collect", func(context.Context) (string, error) {
			return "recovered fixture observation", nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("RP_TEST_RECOVERY_READY"), []byte("ready"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
		t.Fatal("fixture worker was not interrupted")
	}

	d := newTestTask(t, db)
	ready := filepath.Join(t.TempDir(), "checkpoint-ready")
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	worker := func(phase string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessRecoveryIntegration$", "-test.v")
		cmd.Env = append(os.Environ(),
			"RP_TEST_RECOVERY_PHASE="+phase, "RP_TEST_RECOVERY_READY="+ready,
			"RP_TEST_RECOVERY_TASK="+d.Task, "RP_TEST_RECOVERY_TENANT="+d.Tenant,
			fmt.Sprintf("RP_TEST_RECOVERY_GENERATION=%d", d.Generation))
		return cmd
	}
	first := worker("checkpoint")
	var output bytes.Buffer
	first.Stdout, first.Stderr = &output, &output
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if first.ProcessState == nil {
			_ = first.Process.Kill()
			_ = first.Wait()
		}
	}()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			_ = first.Wait()
			t.Fatalf("worker did not save checkpoint: %s", output.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	t.Log("stopped first worker after it committed the collect checkpoint")

	// Advance this fixture task's lease to avoid waiting for its normal expiry.
	if _, err := db.ExecContext(ctx, "UPDATE research_tasks SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE tenant_id=? AND id=?", d.Tenant, d.Task); err != nil {
		t.Fatal(err)
	}
	if err := runner.recoverTask(ctx, d); err != nil {
		t.Fatal(err)
	}
	d.Generation++
	if logs, err := worker("resume").CombinedOutput(); err != nil {
		t.Fatalf("replacement worker failed: %v\n%s", err, logs)
	}
	var reports, reuses, collectStarts int
	err := db.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM research_reports WHERE tenant_id=? AND task_id=? AND markdown LIKE '%recovered fixture observation%'),
 SUM(event_type='checkpoint_reused' AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.step_key'))='collect'),
 SUM(event_type='step_started' AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.step_key'))='collect')
FROM task_events WHERE tenant_id=? AND task_id=?`, d.Tenant, d.Task, d.Tenant, d.Task).Scan(&reports, &reuses, &collectStarts)
	if err != nil || reports != 1 || reuses != 1 || collectStarts != 1 {
		t.Fatalf("reports=%d reused=%d collect_starts=%d err=%v", reports, reuses, collectStarts, err)
	}
	t.Log("replacement worker reused collect once and published one report")
}
