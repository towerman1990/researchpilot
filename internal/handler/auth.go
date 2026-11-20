package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/route"

	"researchpilot/internal/common/envconfig"
	"researchpilot/internal/common/requestmeta"
	"researchpilot/internal/service"
)

type RateLimiter interface {
	Allow(context.Context, string, string, int) (bool, error)
}
type Auth struct {
	Store   service.AuthStore
	Origin  string
	Limiter RateLimiter
}

const sessionCookie = "rp_session"
const tenantKey = "researchpilot.tenant"

func (h Auth) RegisterRoutes(r route.IRouter) {
	r.POST("/api/v1/auth/register", h.requireJSON, h.register)
	r.POST("/api/v1/auth/login", h.requireJSON, h.login)
	r.GET("/api/v1/auth/session", h.apiHeaders, h.requireSession, h.session)
	r.POST("/api/v1/auth/logout", h.requireJSON, h.requireSession, h.logout)
}
func problem(c *app.RequestContext, status int, code string) {
	c.JSON(status, map[string]string{"code": code})
}
func internalError(c *app.RequestContext, err error) {
	log.Printf("API error: %T", err)
	problem(c, 500, "internal_error")
}
func setAPIHeaders(c *app.RequestContext) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
}
func (h Auth) apiHeaders(_ context.Context, c *app.RequestContext) { setAPIHeaders(c) }
func (h Auth) requireJSON(_ context.Context, c *app.RequestContext) {
	setAPIHeaders(c)
	origin := string(c.GetHeader("Origin"))
	if (origin != "" && origin != h.Origin) || string(c.GetHeader("Sec-Fetch-Site")) == "cross-site" {
		problem(c, 403, "origin_forbidden")
		c.Abort()
		return
	}
	media, _, _ := mime.ParseMediaType(string(c.GetHeader("Content-Type")))
	if media != "application/json" {
		problem(c, 415, "json_required")
		c.Abort()
	}
}
func parseJSON(c *app.RequestContext, dst any) bool {
	return parseJSONLimit(c, dst, 8192)
}
func parseJSONLimit(c *app.RequestContext, dst any, limit int) bool {
	body := c.Request.Body()
	if len(body) <= limit {
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		if d.Decode(dst) == nil && d.Decode(new(any)) == io.EOF {
			return true
		}
	}
	problem(c, 422, "invalid_json")
	return false
}
func (h Auth) allow(ctx context.Context, c *app.RequestContext, operation, subject string, limit int) bool {
	if h.Limiter == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ok, err := h.Limiter.Allow(ctx, operation, subject, limit)
	if err != nil {
		problem(c, 503, "rate_limiter_unavailable")
		return false
	}
	if !ok {
		c.Header("Retry-After", "60")
		problem(c, 429, "rate_limited")
	}
	return ok
}

type credentials struct {
	Email    string
	Password string
}

func (h Auth) readCredentials(ctx context.Context, c *app.RequestContext, input *credentials) bool {
	var forwarded []string
	for _, value := range c.Request.Header.PeekAll("X-Forwarded-For") {
		forwarded = append(forwarded, string(value))
	}
	ip := requestmeta.ClientAddress(ctx, c.RemoteAddr().String(), forwarded)
	return h.allow(ctx, c, "auth-ip", ip, envconfig.Int("RATE_AUTH_IP", 30, 1, 10000)) && parseJSON(c, input)
}
func (h Auth) register(ctx context.Context, c *app.RequestContext) {
	var input credentials
	if !h.readCredentials(ctx, c, &input) {
		return
	}
	err := service.Register(ctx, h.Store, input.Email, input.Password)
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		problem(c, 422, "invalid_credentials")
	case errors.Is(err, service.ErrRegistrationConflict):
		problem(c, 409, "registration_conflict")
	case err != nil:
		internalError(c, err)
	default:
		c.JSON(201, map[string]bool{"created": true})
	}
}
func (h Auth) login(ctx context.Context, c *app.RequestContext) {
	var input credentials
	if !h.readCredentials(ctx, c, &input) || !h.allow(ctx, c, "login-account", service.AccountSubject(input.Email), envconfig.Int("RATE_LOGIN_ACCOUNT", 10, 1, 10000)) {
		return
	}
	token, err := service.Login(ctx, h.Store, input.Email, input.Password)
	if errors.Is(err, service.ErrInvalidCredentials) {
		problem(c, 401, "invalid_credentials")
		return
	}
	if err != nil {
		internalError(c, err)
		return
	}
	h.setCookie(c, token, service.SessionAgeSeconds)
	h.session(ctx, c)
}
func (h Auth) requireSession(ctx context.Context, c *app.RequestContext) {
	token := string(c.Cookie(sessionCookie))
	if token == "" {
		problem(c, 401, "authentication_required")
		c.Abort()
		return
	}
	user, err := service.SessionUser(ctx, h.Store, token)
	if errors.Is(err, sql.ErrNoRows) {
		problem(c, 401, "authentication_required")
		c.Abort()
		return
	}
	if err != nil {
		internalError(c, err)
		c.Abort()
		return
	}
	c.Set(tenantKey, user)
}
func (h Auth) session(_ context.Context, c *app.RequestContext) {
	c.JSON(200, map[string]bool{"authenticated": true})
}
func (h Auth) logout(ctx context.Context, c *app.RequestContext) {
	if err := service.Logout(ctx, h.Store, string(c.Cookie(sessionCookie))); err != nil {
		internalError(c, err)
		return
	}
	h.setCookie(c, "", -1)
	c.SetStatusCode(204)
}
func (h Auth) setCookie(c *app.RequestContext, token string, age int) {
	c.SetCookie(sessionCookie, token, age, "/", "", protocol.CookieSameSiteStrictMode, strings.HasPrefix(h.Origin, "https://"), true)
}
