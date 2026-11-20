package service

import (
	"context"
	"database/sql"
	"errors"

	"researchpilot/internal/repository"
)

var ErrModelProfileNotFound = errors.New("model_profile_not_found")
var ErrModelProfileChanged = errors.New("model_profile_changed")

func (s Models) ActivateProfile(ctx context.Context, tenant string, id uint32) error {
	return s.Store.Write(ctx, tenant, true, func(tx repository.WriteTx) error {
		_, err := tx.ModelProfile(tenant, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrModelProfileNotFound
		}
		if err != nil {
			return err
		}
		return tx.SetActiveModelProfile(tenant, id)
	})
}
