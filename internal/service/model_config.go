package service

import (
	"context"
	"net/http"

	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
)

type ModelStore interface {
	Write(context.Context, string, bool, func(repository.WriteTx) error) error
	ActiveModelVersion(context.Context, string) (uint32, error)
	Credential(context.Context, string, uint32) (model.Credential, error)
	ModelStatus(context.Context, string) (*model.ModelSummary, int, error)
	RevokeModels(context.Context, string) error
}
type Models struct {
	Store      ModelStore
	HTTPClient *http.Client
}

func (s Models) Status(ctx context.Context, tenant string) (map[string]any, error) {
	config, used, err := s.Store.ModelStatus(ctx, tenant)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"configured": false, "trial_enabled": TrialEnabled(), "trial_remaining": max(0, TrialLimit-used), "trial_limit": TrialLimit}
	if config != nil {
		result["configured"], result["revoked"] = !config.Revoked, config.Revoked
		result["version"], result["base_url"], result["model"], result["key_tail"] = config.Version, config.BaseURL, config.Model, config.KeyTail
	}
	return result, nil
}
func (s Models) Revoke(ctx context.Context, tenant string) error {
	return s.Store.RevokeModels(ctx, tenant)
}
