package main

import (
	"net/url"
	"testing"
)

func TestConfiguredAppURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{name: "production HTTPS", raw: "https://researchpilot.example/", ok: true},
		{name: "localhost development", raw: "http://localhost:5173/", ok: true},
		{name: "loopback development", raw: "http://127.0.0.1:8080/", ok: true},
		{name: "plain HTTP public host", raw: "http://researchpilot.example/"},
		{name: "credentials", raw: "https://user:secret@researchpilot.example/"},
		{name: "query string", raw: "https://researchpilot.example/?token=secret"},
		{name: "fragment", raw: "https://researchpilot.example/#session"},
		{name: "non-web protocol", raw: "javascript:alert(1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := parseAppURL(tt.raw)
			if (err == nil) != tt.ok {
				t.Fatalf("parseAppURL(%q) error = %v, want success %v", tt.raw, err, tt.ok)
			}
			if err == nil && u == nil {
				t.Fatal("successful URL parse returned nil URL")
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	base, _ := url.Parse("https://researchpilot.example:443/")
	tests := []struct {
		candidate string
		want      bool
	}{
		{"https://researchpilot.example:443/tasks", true},
		{"https://researchpilot.example/tasks", false},
		{"https://researchpilot.example.evil.test/", false},
		{"http://researchpilot.example:443/", false},
		{"https://user@researchpilot.example:443/", false},
		{"javascript:alert(1)", false},
	}
	for _, tt := range tests {
		if got := sameOrigin(base, tt.candidate); got != tt.want {
			t.Errorf("sameOrigin(%q) = %v, want %v", tt.candidate, got, tt.want)
		}
	}
}

func TestIsWebURL(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want bool
	}{
		{"https://example.com/path", true},
		{"http://example.com/path", true},
		{"javascript:alert(1)", false},
		{"file:///C:/secret.txt", false},
		{"https://user:secret@example.com/", false},
	} {
		if got := isWebURL(tt.raw); got != tt.want {
			t.Errorf("isWebURL(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
