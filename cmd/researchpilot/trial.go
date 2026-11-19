package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
)

const trialLimit = 10

func trialEnabled() bool { return os.Getenv("TRIAL_MODEL_ENABLED") == "true" }

// createTrialModel snapshots the server credential using the same encryption and
// immutable versioning as user credentials. The caller holds the user row lock.
func createTrialModel(ctx context.Context, tx *sql.Tx, tenant, task string) (uint32, error) {
	var used int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM trial_usage WHERE tenant_id=?", tenant).Scan(&used); err != nil {
		return 0, err
	}
	if used >= trialLimit {
		return 0, errors.New("trial_exhausted")
	}
	keyBytes, err := os.ReadFile(os.Getenv("TRIAL_MODEL_API_KEY_FILE"))
	if err != nil {
		return 0, errors.New("trial_unavailable")
	}
	key := strings.TrimSpace(string(keyBytes))
	if len(key) < 8 || len(key) > 512 || strings.ContainsAny(key, "\r\n") {
		return 0, errors.New("trial_unavailable")
	}
	base := strings.TrimRight(env("TRIAL_MODEL_BASE_URL", "https://api.deepseek.com"), "/")
	name := env("TRIAL_MODEL_NAME", "deepseek-flash")
	if _, err = providerFor(nil, modelConfig{BaseURL: base, Model: name, Key: key}); err != nil {
		return 0, errors.New("trial_unavailable")
	}
	aead, err := modelCipher()
	if err != nil {
		return 0, errors.New("trial_unavailable")
	}
	var version uint32
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM model_configs WHERE tenant_id=?", tenant).Scan(&version); err != nil {
		return 0, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return 0, err
	}
	encrypted := aead.Seal(nonce, nonce, []byte(key), []byte(fmt.Sprintf("%s:%d", tenant, version)))
	if _, err = tx.ExecContext(ctx, "INSERT INTO model_configs(tenant_id,version,base_url,model,key_cipher,key_tail,key_version,is_trial) VALUES(?,?,?,?,?,?,?,true)", tenant, version, base, name, encrypted, "", currentKeyVersion()); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO trial_usage(tenant_id,task_id) VALUES(?,?)", tenant, task); err != nil {
		return 0, err
	}
	return version, nil
}
