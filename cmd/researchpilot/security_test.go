package main

import (
	"net"
	"testing"
)

func TestOutboundAddressPolicy(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.1.0.2", "169.254.169.254", "::1", "::ffff:127.0.0.1", "100.64.0.1", "198.18.0.1", "2002:7f00:1::"} {
		if publicIP(net.ParseIP(value)) {
			t.Fatalf("unsafe IP allowed: %s", value)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IPv4 denied")
	}
	for _, value := range []string{"file:///etc/passwd", "http://localhost", "http://u:p@example.com", "https://example.com:22"} {
		if _, err := validateURL(value); err == nil {
			t.Fatalf("unsafe URL allowed: %s", value)
		}
	}
}
func TestReportCitationsAreTaskBound(t *testing.T) {
	var draft reportDraft
	draft.Findings = append(draft.Findings, struct {
		Text    string   `json:"text"`
		Sources []string `json:"sources"`
	}{Text: "claim", Sources: []string{"invented"}})
	if _, _, err := renderReport(draft, []evidence{{ID: "real", Title: "title", URL: "https://example.com"}}); err == nil {
		t.Fatal("invented reference accepted")
	}
}
