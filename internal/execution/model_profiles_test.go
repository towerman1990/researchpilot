package execution

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	"researchpilot/internal/service"
	taskservice "researchpilot/internal/task"
)

func TestModelProfilesIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := t.Context()
	t.Setenv("TRIAL_MODEL_ENABLED", "false")
	if os.Getenv("MODEL_MASTER_KEY_FILE") == "" {
		t.Setenv("MODEL_MASTER_KEY_FILE", testMaster(t))
	}
	user, other := newTestTask(t, db), newTestTask(t, db)
	store := executionStore(t, db)
	models := service.Models{Store: store}
	tasks := taskservice.TaskActions{Store: store}
	first := service.ModelInput{BaseURL: "https://api.deepseek.com", Model: "first-model", APIKey: "fixture-first-key"}
	firstVersion, err := models.Save(ctx, user.Tenant, first)
	if err != nil {
		t.Fatal(err)
	}
	oldTask, err := tasks.Submit(ctx, user.Tenant, "first-profile", "First task", "", false)
	if err != nil {
		t.Fatal(err)
	}
	label := "备用配置"
	second := service.ModelInput{BaseURL: "https://api.openai.com/v1", Model: "second-model", APIKey: "fixture-second-key", NewProfile: true, Label: &label}
	secondVersion, err := models.Save(ctx, user.Tenant, second)
	if err != nil {
		t.Fatal(err)
	}
	secondTask, err := tasks.Submit(ctx, user.Tenant, "second-profile", "Second task", "", false)
	if err != nil {
		t.Fatal(err)
	}
	active, profiles, _, err := store.ModelStatus(ctx, user.Tenant)
	if err != nil || active == nil || active.Version != secondVersion || len(profiles) != 2 ||
		profiles[0].Label != label || !profiles[0].IsActive || profiles[1].IsActive {
		t.Fatalf("unexpected profile list: %v", err)
	}
	if err := models.ActivateProfile(ctx, other.Tenant, firstVersion); !errors.Is(err, service.ErrModelProfileNotFound) {
		t.Fatalf("foreign profile selection: %v", err)
	}
	if err := models.ActivateProfile(ctx, user.Tenant, firstVersion); err != nil {
		t.Fatal(err)
	}
	first.APIKey = ""
	first.ExpectedVersion = secondVersion
	if _, err := models.Save(ctx, user.Tenant, first); !errors.Is(err, service.ErrModelProfileChanged) {
		t.Fatalf("stale edit: %v", err)
	}
	first.ExpectedVersion = firstVersion
	changedHost := first
	changedHost.BaseURL = second.BaseURL
	if _, err := models.Save(ctx, user.Tenant, changedHost); !errors.Is(err, service.ErrAPIKeyRequired) {
		t.Fatalf("key reuse with changed base URL: %v", err)
	}
	first.Model = "updated-first-model"
	edited, err := models.Save(ctx, user.Tenant, first)
	if err != nil {
		t.Fatal(err)
	}
	active, profiles, _, err = store.ModelStatus(ctx, user.Tenant)
	if err != nil || active == nil || active.Version != edited || len(profiles) != 2 || profiles[0].ID != firstVersion {
		t.Fatalf("edit changed profile identity: %v", err)
	}
	newTask, err := tasks.Submit(ctx, user.Tenant, "edited-profile", "New task", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		want uint32
	}{
		{oldTask.ID, firstVersion}, {secondTask.ID, secondVersion}, {newTask.ID, edited},
	} {
		var stored uint32
		if err := db.QueryRowContext(ctx, "SELECT model_version FROM research_tasks WHERE tenant_id=? AND id=?", user.Tenant, tc.id).Scan(&stored); err != nil || stored != tc.want {
			t.Fatalf("task's model version changed: got %d, want %d, err %v", stored, tc.want, err)
		}
	}
	oldConfig, err := service.LoadModel(ctx, store, user.Tenant, firstVersion)
	if err != nil || oldConfig.Key != "fixture-first-key" || oldConfig.Model != "first-model" {
		t.Fatalf("old model config should remain available with its original credentials: %v", err)
	}
	if err := models.Revoke(ctx, user.Tenant); err != nil {
		t.Fatal(err)
	}
	if version, err := store.ActiveModelVersion(ctx, user.Tenant); err != nil || version != 0 {
		t.Fatalf("revoked model remains active: %d %v", version, err)
	}
	if _, err := service.LoadModel(ctx, store, user.Tenant, firstVersion); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("revoked credential version can still be loaded: %v", err)
	}
}
