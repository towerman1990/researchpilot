package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"researchpilot/internal/tools"
	"strconv"
	"strings"
)

type Step struct {
	tools.Input
	ID           string   `json:"step_id,omitempty"`
	Question     string   `json:"question,omitempty"`
	Dependencies []string `json:"depends_on,omitempty"`
	Success      string   `json:"success_condition,omitempty"`
}
type Plan struct {
	Version     int      `json:"plan_version,omitempty"`
	Goal        string   `json:"goal,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
	Steps       []Step   `json:"steps"`
}

var validID = regexp.MustCompile("^[A-Za-z0-9_-]{1,32}$")

func ParsePlan(raw string, version int) (Plan, error) {
	var p Plan
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	invalid := func() (Plan, error) { return Plan{}, errors.New("invalid_plan_schema") }
	if len(raw) > 32768 || d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || len(p.Steps) == 0 || len(p.Steps) > 4 || (p.Version != 0 && p.Version != version) || len(p.Goal) > 8000 || len(p.Constraints) > 8 {
		return invalid()
	}
	p.Version = version
	byID := map[string]int{}
	for i := range p.Steps {
		s := &p.Steps[i]
		if s.ID == "" {
			s.ID = strconv.Itoa(i + 1)
		}
		if !validID.MatchString(s.ID) || len(s.Question) > 2000 || len(s.Success) > 2000 || len(s.Dependencies) > 4 {
			return invalid()
		}
		if _, exists := byID[s.ID]; exists {
			return invalid()
		}
		byID[s.ID] = i
		if err := s.Input.Validate(); err != nil {
			return Plan{}, err
		}
		if strings.TrimSpace(s.Success) == "" {
			s.Success = "取得可归档且与研究问题相关的资料"
		}
	}
	for _, s := range p.Steps {
		seen := map[string]bool{}
		for _, id := range s.Dependencies {
			if _, exists := byID[id]; !exists || id == s.ID || seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	}
	// Stable topological ordering. A cycle cannot produce a runnable step.
	ordered := make([]Step, 0, len(p.Steps))
	done := map[string]bool{}
	for len(ordered) < len(p.Steps) {
		progressed := false
		for _, s := range p.Steps {
			if done[s.ID] {
				continue
			}
			ready := true
			for _, id := range s.Dependencies {
				if !done[id] {
					ready = false
				}
			}
			if ready {
				ordered = append(ordered, s)
				done[s.ID] = true
				progressed = true
			}
		}
		if !progressed {
			return invalid()
		}
	}
	p.Steps = ordered
	return p, nil
}
