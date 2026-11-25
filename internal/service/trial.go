package service

import (
	"errors"
	"os"
	"strings"

	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/common/modelkey"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
)

const TrialLimit = 10
const GuestTrialLimit = 3

var ErrGuestTrialExhausted = errors.New("guest_trial_exhausted")

func TrialEnabled() bool { return os.Getenv("TRIAL_MODEL_ENABLED") == "true" }

var ErrTrialExhausted = errors.New("trial_exhausted")
var ErrTrialUnavailable = errors.New("trial_unavailable")

func CreateTrial(tx repository.WriteTx, tenant, id string) (uint32, error) {
	used, err := tx.TrialCount(tenant)
	if err != nil {
		return 0, err
	}
	if used >= TrialLimit {
		return 0, ErrTrialExhausted
	}
	version, err := CreateTrialCredential(tx, tenant)
	if err != nil {
		return 0, err
	}
	return version, tx.RecordTrial(tenant, id)
}

func CreateTrialCredential(tx repository.WriteTx, tenant string) (uint32, error) {
	raw, err := os.ReadFile(os.Getenv("TRIAL_MODEL_API_KEY_FILE"))
	if err != nil {
		return 0, ErrTrialUnavailable
	}
	key := strings.TrimSpace(string(raw))
	if !validKey(key) {
		return 0, ErrTrialUnavailable
	}
	base := strings.TrimRight(envconfig.String("TRIAL_MODEL_BASE_URL", "https://api.deepseek.com"), "/")
	name := envconfig.String("TRIAL_MODEL_NAME", "deepseek-flash")
	if _, err := ProviderFor(nil, ModelConfig{BaseURL: base}); err != nil {
		return 0, ErrTrialUnavailable
	}
	keyVersion := modelkey.Version()
	aead, err := modelkey.Cipher(keyVersion)
	if err != nil {
		return 0, ErrTrialUnavailable
	}
	version, err := tx.NextModelVersion(tenant)
	if err != nil {
		return 0, err
	}
	if err := saveCredential(tx, aead, tenant, model.Credential{Version: version, BaseURL: base, Model: name, KeyVersion: keyVersion}, key, true); err != nil {
		return 0, err
	}
	return version, nil
}
