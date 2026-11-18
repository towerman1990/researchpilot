package main

import (
	"strings"
	"testing"
)

func TestRepeatedURLCannotCrowdOutComplementaryEvidence(t *testing.T) {
	sources := []evidence{{ID: "shutdown", URL: "https://pkg.go.dev/net/http#Server.Shutdown", Excerpt: "Shutdown closes listeners and drains active connections."}}
	for i := 0; i < 6; i++ {
		sources = append(sources, evidence{ID: fmtSeq(uint64(i + 1)), URL: "https://pkg.go.dev/net/http", Excerpt: strings.Repeat("Server headers ", 100) + fmtSeq(uint64(i))})
	}
	selected := promptEvidence(sources, "Compare timeouts and shutdown https://pkg.go.dev/net/http", 16000, 4)
	found := false
	generic := 0
	for _, s := range selected {
		if s.ID == "shutdown" {
			found = true
		}
		if s.URL == "https://pkg.go.dev/net/http" {
			generic++
		}
	}
	if !found || generic != 1 {
		t.Fatal("complementary source lost", selected)
	}
}
