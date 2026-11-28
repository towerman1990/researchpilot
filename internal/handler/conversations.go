package handler

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"researchpilot/internal/repository"
)

func (h Tasks) deleteConversation(ctx context.Context, c *app.RequestContext) {
	var input struct{}
	if !parseJSON(c, &input) {
		return
	}
	if err := h.Actions.DeleteConversation(ctx, c.GetString(tenantKey), c.Param("id")); err != nil {
		writeFailure(c, err)
		return
	}
	c.Status(204)
}

func (h Tasks) conversations(ctx context.Context, c *app.RequestContext) {
	rows, next, err := h.Service.Store.Conversations(ctx, c.GetString(tenantKey), c.Query("before"))
	if errors.Is(err, repository.ErrInvalidConversationCursor) {
		problem(c, 422, "invalid_conversation_cursor")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	if next != "" {
		c.Header("X-Next-Cursor", next)
	}
	c.JSON(200, rows)
}

func (h Tasks) conversation(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if _, err := hex.DecodeString(id); len(id) != 32 || err != nil {
		problem(c, 404, "conversation_not_found")
		return
	}
	var before uint64
	if value := c.Query("before"); value != "" {
		var err error
		before, err = strconv.ParseUint(value, 10, 64)
		if err != nil || before == 0 {
			problem(c, 422, "invalid_conversation_cursor")
			return
		}
	}
	tenant := c.GetString(tenantKey)
	conversation, err := h.Service.Store.Conversation(ctx, tenant, id)
	if errors.Is(err, repository.ErrConversationNotFound) {
		problem(c, 404, "conversation_not_found")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	rows, err := h.Service.Store.ConversationTasks(ctx, tenant, id, before)
	if err != nil {
		internalError(c, err)
		return
	}
	next := ""
	if len(rows) > 20 {
		rows = rows[:20]
		next = strconv.FormatUint(rows[19].ConversationTurn, 10)
	}
	slices.Reverse(rows)
	c.JSON(200, map[string]any{"conversation": conversation, "tasks": rows, "next_before": next})
}
