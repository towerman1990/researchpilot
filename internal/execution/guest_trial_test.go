package execution

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/service"
	taskservice "researchpilot/internal/task"
)

func TestGuestTrialQuotaIntegration(t *testing.T) {
	db := integrationDB(t)
	store := executionStore(t, db)
	actions := taskservice.TaskActions{Store: store}
	t.Setenv("TRIAL_MODEL_ENABLED", "true")
	t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	keyPath := filepath.Join(t.TempDir(), "trial-key")
	const trialKey = "test-only-guest-trial-key"
	if err := os.WriteFile(keyPath, []byte(trialKey), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIAL_MODEL_API_KEY_FILE", keyPath)

	tenant, address := identity.New(16), "guest-quota-test-"+identity.New(16)
	options := taskservice.SubmitOptions{GuestAddress: address}
	ctx := t.Context()
	const key = "guest-idempotency-key"
	const goal = "Guest quota test"
	first, err := actions.Submit(ctx, tenant, key, goal, "", options)
	if err != nil {
		t.Fatal(err)
	}

	var version uint32
	if err := db.QueryRowContext(ctx, "SELECT model_version FROM research_tasks WHERE tenant_id=? AND id=?", tenant, first.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	credential, err := loadModel(ctx, db, tenant, version)
	if err != nil || credential.Key != trialKey {
		t.Fatalf("guest trial credential cannot be decrypted: %v", err)
	}

	// Separate browser sessions on the same address share the limit.
	var wg sync.WaitGroup
	results := make(chan error, 5)
	for i := range cap(results) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := actions.Submit(ctx, identity.New(16), fmt.Sprintf("guest-request-%d", i), goal, "", options)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted, denied := 0, 0
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, service.ErrGuestTrialExhausted):
			denied++
		default:
			t.Fatal(err)
		}
	}
	if accepted != 4 || denied != 1 {
		t.Fatalf("accepted=%d denied=%d", accepted, denied)
	}
	if replay, err := actions.Submit(ctx, tenant, key, goal, "", options); err != nil || replay.ID != first.ID {
		t.Fatalf("replay id=%s err=%v", replay.ID, err)
	}
	if used, err := store.GuestTrialUsed(ctx, tenant, address); err != nil || used != service.GuestTrialLimit {
		t.Fatalf("quota used=%d err=%v", used, err)
	}
	options.GuestAddress += "-new"
	for _, key := range []string{"guest-new-address-1", "guest-new-address-2", "guest-new-address-3", "guest-new-address-4"} {
		if _, err := actions.Submit(ctx, tenant, key, goal, "", options); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := actions.Submit(ctx, tenant, "guest-new-address-5", goal, "", options); !errors.Is(err, service.ErrGuestTrialExhausted) {
		t.Fatalf("browser limit after changing address: %v", err)
	}
	if used, err := store.GuestTrialUsed(ctx, identity.New(16), options.GuestAddress); err != nil || used != 4 {
		t.Fatalf("rejected submission should roll back address quota: used=%d err=%v", used, err)
	}
}
