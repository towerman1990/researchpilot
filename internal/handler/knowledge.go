package handler

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"

	"researchpilot/internal/repository"
	taskservice "researchpilot/internal/task"
)

type Knowledge struct {
	Store *repository.Store
	Auth  Auth
}

func (h Knowledge) RegisterRoutes(r route.IRouter) {
	r.GET("/api/v1/knowledge/libraries", h.Auth.apiHeaders, h.Auth.requireSession, h.list)
	r.POST("/api/v1/knowledge/libraries", h.Auth.requireJSON, h.Auth.requireSession, h.create)
	r.GET("/api/v1/knowledge/libraries/:id/documents", h.Auth.apiHeaders, h.Auth.requireSession, h.documents)
	r.POST("/api/v1/knowledge/libraries/:id/documents", h.Auth.requireJSON, h.Auth.requireSession, h.upload)
}

func (h Knowledge) list(ctx context.Context, c *app.RequestContext) {
	libraries, err := h.Store.KnowledgeLibraries(ctx, c.GetString(tenantKey))
	if err != nil {
		h.failure(c, err)
		return
	}
	c.JSON(200, libraries)
}

func (h Knowledge) create(ctx context.Context, c *app.RequestContext) {
	var input struct{ Name string }
	if !parseJSON(c, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !utf8.ValidString(input.Name) || input.Name == "" || utf8.RuneCountInString(input.Name) > 120 || strings.IndexFunc(input.Name, unicode.IsControl) >= 0 {
		problem(c, 422, "invalid_knowledge_name")
		return
	}
	library, err := h.Store.CreateKnowledgeLibrary(ctx, c.GetString(tenantKey), input.Name)
	if err != nil {
		h.failure(c, err)
		return
	}
	c.JSON(201, library)
}

func (h Knowledge) documents(ctx context.Context, c *app.RequestContext) {
	id := knowledgeID(c)
	if id == "" {
		return
	}
	versions, err := h.Store.KnowledgeVersions(ctx, c.GetString(tenantKey), id)
	if err != nil {
		h.failure(c, err)
		return
	}
	c.JSON(200, versions)
}

func (h Knowledge) upload(ctx context.Context, c *app.RequestContext) {
	id := knowledgeID(c)
	if id == "" {
		return
	}
	var file taskservice.Material
	if !parseJSONLimit(c, &file, 160<<10) {
		return
	}
	file, err := taskservice.NormalizeMaterial(file)
	if err != nil {
		writeFailure(c, err)
		return
	}
	version, err := h.Store.AddKnowledgeVersion(ctx, c.GetString(tenantKey), id, file.Name, file.Content)
	if err != nil {
		h.failure(c, err)
		return
	}
	c.JSON(202, version)
}

func knowledgeID(c *app.RequestContext) string {
	id := c.Param("id")
	if _, err := hex.DecodeString(id); len(id) != 32 || err != nil {
		problem(c, 404, "knowledge_library_not_found")
		return ""
	}
	return strings.ToLower(id)
}

func (h Knowledge) failure(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		problem(c, 404, "knowledge_library_not_found")
	case errors.Is(err, repository.ErrKnowledgeLimit):
		problem(c, 409, err.Error())
	default:
		internalError(c, err)
	}
}
