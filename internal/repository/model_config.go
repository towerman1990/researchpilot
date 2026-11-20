package repository

import (
	"context"
	"encoding/hex"
	"fmt"

	"researchpilot/internal/repository/model"
	"researchpilot/internal/repository/query"
)

func (s *Store) ModelStatus(ctx context.Context, tenant string) (*model.ModelSummary, int, error) {
	q := s.q.ModelSummary.WithContext(ctx)
	used, err := q.TrialCount(tenant)
	if err != nil {
		return nil, 0, err
	}
	if len(used) != 1 {
		return nil, 0, fmt.Errorf("expected one trial count, got %d", len(used))
	}
	rows, err := q.LatestPersonalModel(tenant)
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, used[0], nil
	}
	return &rows[0], used[0], nil
}

func (s *Store) RevokeModels(ctx context.Context, tenant string) error {
	return s.Write(ctx, tenant, true, func(tx WriteTx) error { return tx.configs.RevokePersonalModels(tenant) })
}

// RotateCredentials keeps the locked reads and all ciphertext updates in one transaction.
func (s *Store) RotateCredentials(ctx context.Context, target uint32, reencrypt func(model.RotationCredential) ([]byte, error)) error {
	return query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		configs := tx.ModelSummary.WithContext(ctx)
		credentials, err := configs.LockCredentialsForRotation()
		if err != nil {
			return err
		}
		for _, credential := range credentials {
			if credential.KeyVersion == target {
				continue
			}
			ciphertext, err := reencrypt(credential)
			if err != nil {
				return err
			}
			if err := configs.UpdateCredentialKey(credential.TenantID, credential.Version, target, hex.EncodeToString(ciphertext)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ActiveModelVersion(ctx context.Context, tenant string) (uint32, error) {
	return requireOne(s.q.ModelSummary.WithContext(ctx).ActiveVersion(tenant))
}

func (s *Store) Credential(ctx context.Context, tenant string, version uint32) (model.Credential, error) {
	return requireOne(s.q.ModelSummary.WithContext(ctx).FindCredential(tenant, version))
}

func (t WriteTx) NextModelVersion(tenant string) (uint32, error) {
	return requireOne(t.configs.NextVersion(tenant))
}
func (t WriteTx) ActiveModelVersion(tenant string) (uint32, error) {
	return requireOne(t.configs.ActiveVersion(tenant))
}
func (t WriteTx) Credential(tenant string, version uint32) (model.Credential, error) {
	return requireOne(t.configs.FindCredential(tenant, version))
}
func (t WriteTx) TrialCount(tenant string) (int, error) {
	return requireOne(t.configs.TrialCount(tenant))
}
func (t WriteTx) RecordTrial(tenant, id string) error { return t.configs.RecordTrial(tenant, id) }
func (t WriteTx) SaveCredential(tenant string, c model.Credential, cipherHex, tail string, trial bool) error {
	return t.configs.SaveCredential(tenant, c.Version, c.BaseURL, c.Model, cipherHex, tail, c.KeyVersion, trial)
}

func (t WriteTx) ModelRevoked(tenant string, version uint32) (bool, error) {
	return requireOne(t.configs.ModelRevoked(tenant, version))
}

func (s *Store) ModelRevoked(ctx context.Context, tenant string, version uint32) (bool, error) {
	return requireOne(s.q.ModelSummary.WithContext(ctx).ModelRevoked(tenant, version))
}
