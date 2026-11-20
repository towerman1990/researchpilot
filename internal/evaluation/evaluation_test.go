package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"researchpilot/internal/common/modelkey"
	"researchpilot/internal/repository/model"
)

type timeoutStore struct {
	Store
	credential model.Credential
	submitted  []string
}

func (s *timeoutStore) ActiveModelVersion(context.Context, string) (uint32, error) { return 1, nil }
func (s *timeoutStore) Credential(context.Context, string, uint32) (model.Credential, error) {
	return s.credential, nil
}
func (s *timeoutStore) EvaluationCreate(_ context.Context, _, id, _ string, _ uint32) error {
	s.submitted = append(s.submitted, id)
	return nil
}
func (s *timeoutStore) ReadTask(_ context.Context, _ string, id string) (model.TaskSnapshot, error) {
	if id == s.submitted[0] {
		return model.TaskSnapshot{Status: "SUCCEEDED"}, nil
	}
	return model.TaskSnapshot{Status: "QUEUED"}, nil
}
func (s *timeoutStore) ReadReport(context.Context, string, string) (string, error) {
	return "finished report", nil
}
func (s *timeoutStore) EvaluationSources(context.Context, string, string) ([]model.Source, error) {
	return []model.Source{{ID: "source"}}, nil
}
func (s *timeoutStore) EvaluationCitationCount(context.Context, string, string) (int, error) {
	return 1, nil
}

func TestEvaluateTimeoutReturnsFinishedAndUnfinishedCases(t *testing.T) {
	t.Setenv("EVAL_ALLOW_LIVE", "1")
	t.Setenv("EVAL_TENANT_ID", "test-tenant")
	t.Setenv("EVAL_CASE_LIMIT", "2")
	t.Setenv("EVAL_CASE_IDS", "")
	t.Setenv("EVAL_TIMEOUT", "300ms")
	key := filepath.Join(t.TempDir(), "model.key")
	if err := os.WriteFile(key, []byte(strings.Repeat("01", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MODEL_MASTER_KEY_FILE_V1", key)
	cipher, err := modelkey.Cipher(1)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, cipher.NonceSize())
	encrypted := cipher.Seal(nonce, nonce, []byte("fixture"), []byte("test-tenant:1"))
	store := &timeoutStore{credential: model.Credential{Version: 1, Model: "fixture", KeyVersion: 1, KeyCipher: encrypted}}
	var output bytes.Buffer
	if err := New(store).run(t.Context(), &output); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", err)
	}
	var result struct {
		Status     string
		Cases      []evaluationResult
		Unfinished []string `json:"unfinished_task_ids"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "timed_out" || len(result.Cases) != 2 || result.Cases[0].Markdown != "finished report" ||
		len(result.Unfinished) != 1 || result.Unfinished[0] != store.submitted[1] {
		t.Fatalf("unexpected partial report: %+v", result)
	}
}
