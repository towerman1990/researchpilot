package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	agentcontract "researchpilot/internal/agent"
	"researchpilot/internal/common/textutil"
	researchtools "researchpilot/internal/tools/research"
)

const parallelEvidenceSteps = 2

type collectedStep struct {
	sources []evidence
	failure string
}

func (e execution) executeRound(ctx context.Context, round *researchRound) (*researchRound, error) {
	if round.skip {
		return round, nil
	}
	if err := e.setPhase(ctx, "executing", "task_status", map[string]string{"status": "RUNNING", "phase": "executing"}); err != nil {
		return nil, err
	}

	pending := append([]agentcontract.Step(nil), round.plan.Steps...)
	settled := make(map[string]bool, len(pending))
	collected := make(map[string]collectedStep, len(pending))
	for len(pending) > 0 {
		batch := make([]agentcontract.Step, 0, parallelEvidenceSteps)
		remaining := pending[:0]
		for _, step := range pending {
			if len(batch) < parallelEvidenceSteps && dependenciesSettled(step, settled) {
				batch = append(batch, step)
			} else {
				remaining = append(remaining, step)
			}
		}
		if len(batch) == 0 {
			return nil, errors.New("invalid_plan_schema")
		}
		pending = remaining
		results, err := e.collectBatch(ctx, round, batch, settled)
		if err != nil {
			return nil, err
		}
		for i, result := range results {
			collected[batch[i].ID] = result
			settled[batch[i].ID] = len(result.sources) != 0
		}
	}

	// Present results in plan order, independently of tool latency.
	for _, step := range round.plan.Steps {
		result := collected[step.ID]
		round.sources = append(round.sources, result.sources...)
		if len(result.sources) == 0 {
			round.failures += result.failure + " "
			round.roundFailures++
		}
	}
	return round, nil
}

func dependenciesSettled(step agentcontract.Step, settled map[string]bool) bool {
	for _, dependency := range step.Dependencies {
		if _, done := settled[dependency]; !done {
			return false
		}
	}
	return true
}

func (e execution) collectBatch(ctx context.Context, round *researchRound, batch []agentcontract.Step, settled map[string]bool) ([]collectedStep, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	results := make([]collectedStep, len(batch))
	var workers sync.WaitGroup
	for i, step := range batch {
		workers.Go(func() {
			result, err := e.collectPlanStep(ctx, round, step, settled)
			if err != nil {
				cancel(err)
			}
			results[i] = result
		})
	}
	workers.Wait()
	return results, context.Cause(ctx)
}

func (e execution) collectPlanStep(ctx context.Context, round *researchRound, step agentcontract.Step, settled map[string]bool) (collectedStep, error) {
	switch step.Tool {
	case "fetch_page", "github_repo", "web_search", "microsoft_docs_search":
	default:
		return collectedStep{}, errors.New("invalid_plan_tool")
	}
	key := fmt.Sprintf("tool-%d-%s", round.index+1, step.ID)
	saved, err := e.checkpoint(ctx, key, func(ctx context.Context) (string, error) {
		for _, dependency := range step.Dependencies {
			if !settled[dependency] {
				return `{"error":"dependency_failed"}`, nil
			}
		}
		goal := round.state.task.goal + " " + step.Question
		if round.state.knowledgeLibraryID == "" {
			goal += " " + round.failures
		}
		collected, err := e.registry.Execute(researchtools.WithEvidenceGoal(ctx, goal), step.Input)
		if err != nil {
			raw, _ := json.Marshal(map[string]string{"error": err.Error()})
			return string(raw), nil
		}
		raw, err := json.Marshal(collected)
		return string(raw), err
	})
	if err != nil {
		return collectedStep{}, err
	}
	var sources []evidence
	if json.Unmarshal([]byte(saved), &sources) == nil && len(sources) > 0 {
		return collectedStep{sources: sources}, nil
	}
	input, _ := json.Marshal(step.Input)
	return collectedStep{failure: "Attempted " + textutil.Clip(string(input), 1000) + " returned " + textutil.Clip(saved, 256)}, nil
}
