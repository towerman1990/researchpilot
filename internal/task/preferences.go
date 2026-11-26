package task

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"researchpilot/internal/repository"
	"researchpilot/internal/repository/model"
)

var ErrInvalidPreferences = errors.New("invalid_research_preferences")

func (s TaskActions) Preferences(ctx context.Context, tenant string) (model.ResearchPreferences, error) {
	return s.Store.ReadResearchPreferences(ctx, tenant)
}

func (s TaskActions) SavePreferences(ctx context.Context, tenant string, preferences model.ResearchPreferences, memory *string) (model.ResearchPreferences, error) {
	if !slices.Contains([]string{"", "official", "github"}, preferences.Sources) ||
		!slices.Contains([]string{"", "brief", "detailed"}, preferences.Detail) ||
		!slices.Contains([]string{"", "conclusion_first", "bullets"}, preferences.Structure) {
		return model.ResearchPreferences{}, ErrInvalidPreferences
	}
	if memory != nil {
		preferences.Memory = strings.TrimSpace(*memory)
		if utf8.RuneCountInString(preferences.Memory) > 1200 || strings.ContainsRune(preferences.Memory, 0) {
			return model.ResearchPreferences{}, ErrInvalidPreferences
		}
	}
	// Serialize preference changes with task creation on the same account.
	err := s.Store.Write(ctx, tenant, true, func(tx repository.WriteTx) error {
		if memory == nil {
			current, err := tx.ReadResearchPreferences(tenant)
			if err != nil {
				return err
			}
			preferences.Memory = current.Memory
		}
		return tx.SaveResearchPreferences(tenant, preferences)
	})
	return preferences, err
}
