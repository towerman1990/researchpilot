package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"

	"researchpilot/internal/common/identity"
	"researchpilot/internal/service"
)

const guestCookie = "rp_guest"
const guestKey = "researchpilot.guest"

func guestTenant(token string) string {
	if len(token) != 64 {
		return ""
	}
	raw, err := hex.DecodeString(token)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:16])
}

func (h Auth) guestSession(ctx context.Context, c *app.RequestContext) {
	token := string(c.Cookie(guestCookie))
	tenant := guestTenant(token)
	if tenant == "" {
		token = identity.New(32)
		tenant = guestTenant(token)
		c.SetCookie(guestCookie, token, 30*24*3600, "/", "", protocol.CookieSameSiteStrictMode, strings.HasPrefix(h.Origin, "https://"), true)
	}
	used, err := h.Guests.GuestTrialUsed(ctx, tenant, clientAddress(ctx, c))
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(200, map[string]any{
		"guest": true, "trial_enabled": service.TrialEnabled(),
		"trial_limit": service.GuestTrialLimit, "trial_remaining": max(0, service.GuestTrialLimit-used),
	})
}

func (h Auth) requireTaskSession(ctx context.Context, c *app.RequestContext) {
	if len(c.Cookie(sessionCookie)) == 0 {
		if tenant := guestTenant(string(c.Cookie(guestCookie))); tenant != "" {
			c.Set(tenantKey, tenant)
			c.Set(guestKey, true)
			return
		}
	}
	h.requireSession(ctx, c)
}
