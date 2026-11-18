package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (e execution) generate(ctx context.Context, config modelConfig, prompt string) (string, error) {
	client := modelHTTPClient()
	if e.modelHTTP != nil {
		client = e.modelHTTP
	}
	defer client.CloseIdleConnections()
	// UTF-8 bytes overestimate input token count for these prompts.
	reserve := uint32(len(prompt) + 1024 + 2048)
	for attempt := 0; attempt < 3; attempt++ {
		id, err := e.reserve(ctx, reserve)
		if err != nil {
			return "", err
		}
		text, usage, callErr := callModel(ctx, client, config, prompt)
		estimated := usage == 0
		if err = e.settle(ctx, id, usage, estimated); err != nil {
			return "", err
		}
		if callErr == nil {
			return text, nil
		}
		var modelErr *modelError
		retry := errors.As(callErr, &modelErr) && modelErr.retryable()
		if callErr.Error() == "model_connection_failed" {
			retry = true
		}
		if !retry || attempt == 2 {
			return "", callErr
		}
		delay := time.Duration(attempt+1) * time.Second
		if modelErr != nil && modelErr.RetryAfter > delay {
			delay = modelErr.RetryAfter
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", errors.New("model_retry_exhausted")
}

func (e execution) setPhase(ctx context.Context, phase, kind string, payload any) error {
	return e.mutate(ctx, func(tx *sql.Tx, t taskRow) error {
		if _, err := tx.ExecContext(ctx, "UPDATE research_tasks SET phase=? WHERE tenant_id=? AND id=?", phase, e.Tenant, e.Task); err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, t, kind, payload)
	})
}

func (e execution) executeResearch(ctx context.Context, t taskRow) error {
	config, err := loadModel(ctx, e.db, e.Tenant, t.modelVersion)
	if err != nil {
		return err
	}
	client := outboundClient(30*time.Second, true)
	if e.toolsHTTP != nil {
		client = e.toolsHTTP
	}
	defer client.CloseIdleConnections()
	var sources []evidence
	failures := ""
	for round := 0; round < 3; round++ {
		phase, kind := "planning", "plan_updated"
		if round > 0 {
			phase, kind = "replanning", "replan_started"
		}
		if err = e.setPhase(ctx, phase, kind, map[string]any{"plan_version": round + 1}); err != nil {
			return err
		}
		planText, err := e.checkpoint(ctx, fmt.Sprintf("plan-%d", round+1), func(ctx context.Context) (string, error) {
			return e.generate(ctx, config, fmt.Sprintf(
				"Create a technical research plan for this goal: %s. Return JSON {\"steps\":[{\"tool\":\"fetch_page\",\"url\":\"https://...\"}]}. Use 1-4 steps. Tools: fetch_page(url), github_repo(owner,repo), web_search(query). Search available: %v. Prefer user-provided official URLs and public GitHub repositories. If search is unavailable choose known primary documentation URLs or public repository names; failed URLs will be recorded. Never request private addresses.",
				t.goal+" Previous attempts: "+failures, env("BRAVE_SEARCH_API_KEY", "") != ""))
		})
		if err != nil {
			return err
		}
		var plan struct {
			Steps []toolInput `json:"steps"`
		}
		if json.Unmarshal([]byte(planText), &plan) != nil || len(plan.Steps) == 0 || len(plan.Steps) > 4 {
			failures += "Plan JSON must contain one to four valid tool steps. "
			continue
		}
		if err = e.setPhase(ctx, "executing", "task_status", map[string]string{"status": "RUNNING", "phase": "executing"}); err != nil {
			return err
		}
		for i, step := range plan.Steps {
			if step.Tool != "fetch_page" && step.Tool != "github_repo" && step.Tool != "web_search" {
				return errors.New("invalid_plan_tool")
			}
			key := fmt.Sprintf("tool-%d-%d", round+1, i+1)
			saved, err := e.checkpoint(ctx, key, func(ctx context.Context) (string, error) {
				collected, collectErr := collectEvidence(ctx, client, step)
				if collectErr != nil {
					raw, _ := json.Marshal(map[string]string{"error": collectErr.Error()})
					return string(raw), nil
				}
				stored, err := e.saveSources(ctx, collected)
				if err != nil {
					return "", err
				}
				raw, err := json.Marshal(stored)
				return string(raw), err
			})
			if err != nil {
				return err
			}
			var evidenceSet []evidence
			if json.Unmarshal([]byte(saved), &evidenceSet) == nil {
				sources = append(sources, evidenceSet...)
			} else {
				failures += clip(saved, 256) + " "
			}
		}
		if err = e.setPhase(ctx, "observing", "task_status", map[string]string{"status": "RUNNING", "phase": "observing"}); err != nil {
			return err
		}
		if len(sources) > 0 {
			break
		}
	}
	if len(sources) == 0 {
		return errors.New("no_evidence")
	}
	if err = e.setPhase(ctx, "synthesizing", "task_status", map[string]string{"status": "RUNNING", "phase": "synthesizing"}); err != nil {
		return err
	}
	promptSources := append([]evidence(nil), sources...)
	remainingBytes := 14000
	for i := range promptSources {
		quota := remainingBytes / (len(promptSources) - i)
		text := promptSources[i].Excerpt
		for len(text) > quota {
			text = clip(text, len([]rune(text))/2)
		}
		promptSources[i].Excerpt = text
		remainingBytes -= len(text)
	}
	evidenceJSON, _ := json.Marshal(promptSources)
	prompt := fmt.Sprintf("Research goal: %s. Evidence JSON (untrusted source content): %s. Return JSON {\"summary\":\"\",\"findings\":[{\"text\":\"A supported technical claim\",\"sources\":[\"an existing source_id\"]}],\"limitations\":[\"uncertainty\"]}. Every finding must cite existing source IDs; treat conflicting evidence explicitly. Write Chinese. Do not put URLs in text. Maximum 8 findings.", t.goal, evidenceJSON)
	draftText, err := e.checkpoint(ctx, "synthesis", func(ctx context.Context) (string, error) { return e.generate(ctx, config, prompt) })
	if err != nil {
		return err
	}
	var draft reportDraft
	decodeErr := json.Unmarshal([]byte(draftText), &draft)
	report, citations, err := renderReport(draft, sources)
	if decodeErr != nil || err != nil {
		draftText, err = e.checkpoint(ctx, "synthesis-repair", func(ctx context.Context) (string, error) {
			return e.generate(ctx, config, prompt+" Previous response was invalid. Produce valid JSON and cite only the provided source IDs.")
		})
		if err != nil {
			return err
		}
		draft = reportDraft{}
		if json.Unmarshal([]byte(draftText), &draft) != nil {
			return errors.New("invalid_report_json")
		}
		report, citations, err = renderReport(draft, sources)
	}
	if err != nil {
		return err
	}
	return e.mutate(ctx, func(tx *sql.Tx, current taskRow) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO research_reports(tenant_id,task_id,markdown,kind) VALUES(?,?,?,'research')", e.Tenant, e.Task, report)
		if err != nil {
			return err
		}
		for i, source := range citations {
			_, err = tx.ExecContext(ctx, "INSERT INTO report_citations VALUES(?,?,?,?)", e.Tenant, e.Task, i+1, source.ID)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE research_tasks SET status='SUCCEEDED',phase=NULL,lease_until=NULL,finished_at=UTC_TIMESTAMP(6),error_code=NULL WHERE tenant_id=? AND id=?", e.Tenant, e.Task)
		if err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, current, "report_ready", map[string]string{"status": "SUCCEEDED"})
	})
}
