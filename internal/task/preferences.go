package task

import (
	"context"
	"errors"
	"slices"

	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
)

var ErrInvalidPreferences = errors.New("invalid_research_preferences")

func (s TaskActions) Preferences(ctx context.Context, tenant string) (model.ResearchPreferences, error) {
	return s.Store.ReadResearchPreferences(ctx, tenant)
}

func (s TaskActions) SavePreferences(ctx context.Context, tenant string, preferences model.ResearchPreferences) error {
	if !slices.Contains([]string{"", "official", "github"}, preferences.Sources) ||
		!slices.Contains([]string{"", "brief", "detailed"}, preferences.Detail) ||
		!slices.Contains([]string{"", "conclusion_first", "bullets"}, preferences.Structure) {
		return ErrInvalidPreferences
	}
	// Serialize preference changes with task creation on the same account.
	return s.Store.Write(ctx, tenant, true, func(tx repository.WriteTx) error {
		return tx.SaveResearchPreferences(tenant, preferences)
	})
}
