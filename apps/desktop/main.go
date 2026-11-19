package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/egoist/mygo"
)

const (
	defaultAppURL = "https://researchpilot.towerman1990.cn/"
	appTitle      = "ResearchPilot"
)

func main() {
	appURL, err := configuredAppURL()
	if err != nil {
		log.Fatal(err)
	}

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           appTitle,
			URL:             appURL.String(),
			Width:           1360,
			Height:          900,
			MinWidth:        900,
			MinHeight:       620,
			StateKey:        "researchpilot-main",
			BackgroundColor: "#f7f8fa",
			Page: mygo.PageOptions{
				DevTools: mygo.DevToolsDisabled,
			},
		})
		page := win.Page()
		page.OnWillNavigate(func(event *mygo.NavigateEvent) {
			if sameOrigin(appURL, event.URL) {
				return
			}
			event.PreventDefault()
			if event.UserInitiated && isWebURL(event.URL) {
				go mygo.Shell.OpenExternal(event.URL)
			}
		})
		page.SetWindowOpenHandler(func(request mygo.WindowOpenRequest) *mygo.WindowOptions {
			if request.UserInitiated && isWebURL(request.URL) {
				go mygo.Shell.OpenExternal(request.URL)
			}
			return nil
		})
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func configuredAppURL() (*url.URL, error) {
	raw := strings.TrimSpace(os.Getenv("RESEARCHPILOT_DESKTOP_URL"))
	if raw == "" {
		raw = defaultAppURL
	}
	return parseAppURL(raw)
}

func parseAppURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid ResearchPilot URL: expected an HTTPS origin and optional path")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return nil, fmt.Errorf("ResearchPilot desktop requires HTTPS; HTTP is allowed only for localhost development")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u, nil
}

func sameOrigin(base *url.URL, candidate string) bool {
	u, err := url.Parse(candidate)
	if err != nil || u.User != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, base.Scheme) && strings.EqualFold(u.Host, base.Host)
}

func isWebURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http")
}

func isLoopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || strings.EqualFold(host, "127.0.0.1") || strings.EqualFold(host, "::1")
}
