package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"researchpilot/internal/repository/model"
)

func (s *Store) ReadResearchPreferences(ctx context.Context, tenant string) (model.ResearchPreferences, error) {
	var preferences model.ResearchPreferences
	raw, err := requireOne(s.q.TaskSnapshot.WithContext(ctx).ReadResearchPreferences(tenant))
	if errors.Is(err, sql.ErrNoRows) {
		return preferences, nil
	}
	if err != nil {
		return preferences, err
	}
	err = json.Unmarshal([]byte(raw), &preferences)
	return preferences, err
}

func (t WriteTx) SaveResearchPreferences(tenant string, preferences model.ResearchPreferences) error {
	if preferences == (model.ResearchPreferences{}) {
		return t.tasks.ClearResearchPreferences(tenant)
	}
	raw, err := json.Marshal(preferences)
	if err != nil {
		return err
	}
	return t.tasks.SaveResearchPreferences(tenant, string(raw))
}
