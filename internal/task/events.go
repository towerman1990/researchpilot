// Package task contains task lifecycle and event services.
package task

import (
	"context"
	"errors"
	"strconv"
	"time"

	"researchpilot/internal/repository/model"
)

type EventReader interface {
	ReadEventPosition(context.Context, string, string) (model.EventPosition, error)
	ReadEventBatch(context.Context, string, string, uint64) (model.EventPosition, []model.TaskEvent, error)
}
type EventStream struct{ Store EventReader }

var ErrInvalidCursor = errors.New("invalid_event_cursor")

func (s EventStream) Open(ctx context.Context, tenant, id, raw string) (uint64, error) {
	position, err := s.Store.ReadEventPosition(ctx, tenant, id)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || cursor > position.LatestSeq {
		return 0, ErrInvalidCursor
	}
	return cursor, nil
}

type EventSink interface {
	Event(model.TaskEvent) error
	Reset() error
	Heartbeat() error
}

func (s EventStream) Follow(ctx context.Context, tenant, id string, cursor uint64, sink EventSink) error {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		position, batch, err := s.Store.ReadEventBatch(ctx, tenant, id, cursor)
		if err != nil {
			return err
		}
		if CursorExpired(cursor, position.MinSeq) {
			return sink.Reset()
		}
		for _, event := range batch {
			if err := sink.Event(event); err != nil {
				return err
			}
			cursor = event.Seq
		}
		if Terminal(position.Status) && cursor >= position.LatestSeq {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		case <-heartbeat.C:
			if err := sink.Heartbeat(); err != nil {
				return err
			}
		}
	}
}
func CursorExpired(cursor, minimum uint64) bool { return minimum > 1 && cursor < minimum-1 }
func Terminal(status string) bool {
	return status == "SUCCEEDED" || status == "FAILED" || status == "CANCELLED" || status == "PARTIAL"
}
