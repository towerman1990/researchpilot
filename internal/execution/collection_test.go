package execution

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agentcontract "researchpilot/internal/agent"
	toolcontract "researchpilot/internal/tools"
)

func TestParallelEvidenceCollectionIntegration(t *testing.T) {
	db := integrationDB(t)
	e, _ := claimTest(t, db)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	slowStarted, independentStarted := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	registry := toolcontract.NewRegistry[[]evidence]()
	err := registry.Register(toolcontract.Spec{
		Name: "fetch_page", Version: "1", Timeout: 4 * time.Second, InputSchema: toolcontract.Schemas["fetch_page"],
	}, func(ctx context.Context, input toolcontract.Input) ([]evidence, error) {
		calls.Add(1)
		switch input.URL {
		case "https://example.com/slow":
			close(slowStarted)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-independentStarted:
			}
		case "https://example.com/unavailable":
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-slowStarted:
			}
			close(independentStarted)
			return nil, errors.New("fixture_unavailable")
		case "https://example.com/dependent":
			if _, err := e.store.ReadCheckpoint(ctx, e.Tenant, e.Task, "tool-1-slow"); err != nil {
				return nil, errors.New("dependency_checkpoint_missing")
			}
		default:
			return nil, errors.New("unexpected_tool_call")
		}
		return []evidence{{Title: input.URL, URL: input.URL, Excerpt: "Fixture evidence"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	e.registry = registry
	plan, err := agentcontract.ParsePlan(`{"steps":[
		{"step_id":"slow","tool":"fetch_page","url":"https://example.com/slow"},
		{"step_id":"dependent","tool":"fetch_page","url":"https://example.com/dependent","depends_on":["slow"]},
		{"step_id":"unavailable","tool":"fetch_page","url":"https://example.com/unavailable"},
		{"step_id":"skipped","tool":"fetch_page","url":"https://example.com/skipped","depends_on":["unavailable"]}
	]}`, 1)
	if err != nil {
		t.Fatal(err)
	}
	initial := researchRound{state: &researchState{}, plan: plan}
	firstRound := initial
	first, err := e.executeRound(ctx, &firstRound)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.sources) != 2 || first.sources[0].URL != "https://example.com/slow" || first.sources[1].URL != "https://example.com/dependent" ||
		first.roundFailures != 2 || !strings.Contains(first.failures, "fixture_unavailable") ||
		!strings.Contains(first.failures, "dependency_failed") || calls.Load() != 3 {
		t.Fatalf("unexpected collection result: sources=%+v failures=%q tool calls=%d", first.sources, first.failures, calls.Load())
	}
	replayed, err := e.executeRound(ctx, &initial)
	if err != nil || calls.Load() != 3 || !slices.Equal(first.sources, replayed.sources) || first.failures != replayed.failures {
		t.Fatalf("collection did not reuse its checkpoints: result=%+v calls=%d err=%v", replayed, calls.Load(), err)
	}
}
