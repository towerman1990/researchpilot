package service

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"researchpilot/internal/common/modelkey"
	"researchpilot/internal/outbound"
	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
)

var (
	ErrInvalidModelConfig  = errors.New("invalid_model_config")
	ErrModelHostForbidden  = errors.New("model_host_forbidden")
	ErrInvalidAPIKey       = errors.New("invalid_api_key")
	ErrModelEncryption     = errors.New("model_encryption_not_configured")
	ErrAPIKeyRequired      = errors.New("api_key_required")
	ErrModelKeyUnavailable = errors.New("model_key_unavailable")
	ErrModelNotConfigured  = errors.New("model_not_configured")
	ErrModelUnavailable    = errors.New("model_configuration_unavailable")
)

type ModelInput struct {
	BaseURL         string  `json:"base_url"`
	Model           string  `json:"model"`
	APIKey          string  `json:"api_key"`
	Label           *string `json:"label"`
	NewProfile      bool    `json:"new_profile"`
	ExpectedVersion uint32  `json:"expected_version"`
}

func validKey(key string) bool {
	return len(key) >= 8 && len(key) <= 512 && !strings.ContainsAny(key, "\r\n")
}
func openCredential(tenant string, c model.Credential) (string, error) {
	aead, err := modelkey.Cipher(c.KeyVersion)
	if err != nil {
		return "", err
	}
	return modelkey.Open(aead, c.KeyCipher, tenant, c.Version)
}

func saveCredential(tx repository.WriteTx, aead cipher.AEAD, tenant string, c model.Credential, key string, trial bool) error {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	encrypted := aead.Seal(nonce, nonce, []byte(key), []byte(fmt.Sprintf("%s:%d", tenant, c.Version)))
	tail := ""
	if !trial {
		tail = key[len(key)-4:]
	}
	return tx.SaveCredential(tenant, c, hex.EncodeToString(encrypted), tail, trial)
}

func (s Models) Save(ctx context.Context, tenant string, input ModelInput) (uint32, error) {
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	u, err := outbound.ValidateURL(input.BaseURL)
	if err != nil || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" || len(input.BaseURL) > 512 || len(input.Model) == 0 || len(input.Model) > 128 {
		return 0, ErrInvalidModelConfig
	}
	if _, err := ProviderFor(nil, ModelConfig{BaseURL: input.BaseURL}); err != nil {
		return 0, ErrModelHostForbidden
	}
	if input.Label != nil && utf8.RuneCountInString(strings.TrimSpace(*input.Label)) > 80 {
		return 0, ErrInvalidModelConfig
	}
	if input.NewProfile && input.APIKey == "" {
		return 0, ErrAPIKeyRequired
	}
	if input.APIKey != "" && !validKey(input.APIKey) {
		return 0, ErrInvalidAPIKey
	}
	keyVersion := modelkey.Version()
	aead, err := modelkey.Cipher(keyVersion)
	if err != nil {
		return 0, ErrModelEncryption
	}
	var version uint32
	err = s.Store.Write(ctx, tenant, true, func(tx repository.WriteTx) error {
		next, err := tx.NextModelVersion(tenant)
		if err != nil {
			return err
		}
		profile := model.ModelProfile{ID: next}
		if !input.NewProfile {
			previous, err := tx.ActiveModelProfile(tenant)
			if err == nil {
				profile = previous
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if input.ExpectedVersion != 0 && profile.Version != input.ExpectedVersion {
				return ErrModelProfileChanged
			}
		}
		if input.Label != nil {
			profile.Label = strings.TrimSpace(*input.Label)
		}
		if input.APIKey == "" {
			if profile.Version == 0 || input.BaseURL != profile.BaseURL {
				return ErrAPIKeyRequired
			}
			row, err := tx.Credential(tenant, profile.Version)
			if err != nil {
				return ErrModelKeyUnavailable
			}
			input.APIKey, err = openCredential(tenant, row)
			if err != nil || len(input.APIKey) < 4 {
				return ErrModelKeyUnavailable
			}
		}
		if err := saveCredential(tx, aead, tenant, model.Credential{Version: next, BaseURL: input.BaseURL, Model: input.Model, KeyVersion: keyVersion}, input.APIKey, false); err != nil {
			return err
		}
		if err := tx.SaveModelProfile(tenant, profile.ID, next, profile.Label); err != nil {
			return err
		}
		if err := tx.SetActiveModelProfile(tenant, profile.ID); err != nil {
			return err
		}
		version = next
		return nil
	})
	return version, err
}

func LoadModel(ctx context.Context, store interface {
	Credential(context.Context, string, uint32) (model.Credential, error)
}, tenant string, version uint32) (ModelConfig, error) {
	row, err := store.Credential(ctx, tenant, version)
	if err != nil {
		return ModelConfig{}, err
	}
	key, err := openCredential(tenant, row)
	if err != nil {
		return ModelConfig{}, err
	}
	return ModelConfig{Version: row.Version, BaseURL: row.BaseURL, Model: row.Model, Key: key}, nil
}
