package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	taskservice "researchpilot/internal/task"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/protocol/sse"

	"researchpilot/internal/repository/model"
	"researchpilot/internal/service"
)

func (h Tasks) events(ctx context.Context, c *app.RequestContext) {
	tenant, id := c.GetString(tenantKey), c.Param("id")
	raw := string(c.GetHeader("Last-Event-ID"))
	if raw == "" {
		raw = c.Query("after")
	}
	cursor, err := h.Stream.Open(ctx, tenant, id, raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		problem(c, 404, "task_not_found")
		return
	case errors.Is(err, taskservice.ErrInvalidCursor):
		problem(c, 422, "invalid_event_cursor")
		return
	case err != nil:
		internalError(c, err)
		return
	}
	c.Header("X-Accel-Buffering", "no")
	sink := eventSink{writer: sse.NewWriter(c), conn: c.GetConn(), task: id, check: func() error {
		_, err := service.SessionUser(ctx, h.Auth.Store, string(c.Cookie(sessionCookie)))
		return err
	}}
	c.Header("Cache-Control", "no-store")
	defer func() { _ = sink.send(sink.writer.Close) }()
	if err := sink.send(func() error { return sink.writer.WriteComment(" connected") }); err != nil {
		return
	}
	_ = h.Stream.Follow(ctx, tenant, id, cursor, sink)
}

type eventSink struct {
	writer *sse.Writer
	conn   network.Conn
	task   string
	check  func() error
}

func (s eventSink) send(write func() error) error {
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = s.conn.SetWriteDeadline(time.Time{}) }()
	return write()
}
func (s eventSink) Reset() error {
	return s.send(func() error { return s.writer.WriteEvent("", "reset", []byte("{\"reason\":\"cursor_expired\"}")) })
}
func (s eventSink) Heartbeat() error {
	if err := s.check(); err != nil {
		return err
	}
	return s.send(func() error { return s.writer.WriteComment(" heartbeat") })
}
func (s eventSink) Event(e model.TaskEvent) error {
	seq := strconv.FormatUint(e.Seq, 10)
	data, err := json.Marshal(map[string]any{"schema_version": 1, "task_id": s.task, "seq": seq, "payload": e.Payload, "timestamp": e.Timestamp})
	if err != nil {
		return err
	}
	return s.send(func() error { return s.writer.WriteEvent(seq, e.Kind, data) })
}
