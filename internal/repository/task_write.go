package repository

import (
	"context"
	"encoding/hex"

	"researchpilot/internal/repository/model"
)

func (t WriteTx) AddTask(tenant, id, goal string) error { return t.tasks.AddTask(tenant, id, goal) }
func (t WriteTx) SetTaskModel(tenant, id string, version uint32) error {
	return t.tasks.SetTaskModel(tenant, id, version)
}
func (t WriteTx) RequestKey(tenant string, digest [32]byte) (model.RequestKey, error) {
	return requireOne(t.tasks.FindRequestKey(tenant, hex.EncodeToString(digest[:])))
}
func (s *Store) RequestKey(ctx context.Context, tenant string, digest [32]byte) (model.RequestKey, error) {
	return requireOne(s.q.TaskSnapshot.WithContext(ctx).FindRequestKey(tenant, hex.EncodeToString(digest[:])))
}
func (t WriteTx) AddRequestKey(tenant, id string, key, request [32]byte) error {
	return t.tasks.AddRequestKey(tenant, id, hex.EncodeToString(key[:]), hex.EncodeToString(request[:]))
}
func (t WriteTx) AddOutbox(tenant, id string) error { return t.tasks.AddOutbox(tenant, id) }
func (t WriteTx) AppendEvent(tenant, id string, seq uint64, payload string) error {
	return t.tasks.AppendStatusEvent(tenant, id, seq, payload)
}
func (t WriteTx) LockTask(tenant, id string) (model.TaskSnapshot, error) {
	return requireOne(t.tasks.LockTaskStatus(tenant, id))
}
func (t WriteTx) FlagCancellation(tenant, id string) error {
	return t.tasks.FlagCancellation(tenant, id)
}
func (t WriteTx) MarkCancelling(tenant, id string, seq uint64) error {
	return t.tasks.MarkCancelling(tenant, id, seq)
}
func (t WriteTx) MarkCancelled(tenant, id string, seq uint64) error {
	if err := t.tasks.ChargeReservedTokens(tenant, id); err != nil {
		return err
	}
	if err := t.tasks.SettleReservedAttempts(tenant, id); err != nil {
		return err
	}
	if err := t.tasks.AbandonStartedAttempts(tenant, id); err != nil {
		return err
	}
	return t.tasks.MarkCancelled(tenant, id, seq)
}
