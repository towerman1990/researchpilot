package knowledge

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestEvaluateDemoKeywordBaseline(t *testing.T) {
	report, err := evaluateDemo(t.Context(), nil)
	if err != nil || len(report.Strategies) != 1 {
		t.Fatalf("expected keyword baseline: %+v (%v)", report, err)
	}
	strategy := report.Strategies[0]
	if strategy.Mode != "keyword" || len(strategy.Cases) != len(evaluationCases) || strategy.RecallAt5 < strategy.RecallAt1 || strategy.RecallAt5 == 0 {
		t.Fatalf("invalid keyword metrics: %+v", strategy)
	}
}

func TestEvaluateDemoReportsAllStrategies(t *testing.T) {
	embed := func(_ context.Context, texts []string) ([][]float64, error) {
		vectors := make([][]float64, len(texts))
		for n := range vectors {
			vectors[n] = []float64{1, 0}
		}
		return vectors, nil
	}
	report, err := evaluateDemo(t.Context(), embed)
	if err != nil {
		t.Fatal(err)
	}
	var modes []string
	for _, strategy := range report.Strategies {
		modes = append(modes, strategy.Mode)
	}
	if !slices.Equal(modes, []string{"keyword", "vector", "hybrid"}) {
		t.Fatalf("unexpected strategies: %v", modes)
	}
	if report.Strategies[1].UncoveredWithResults != 2 {
		t.Fatal("expected irrelevant vector hits to be counted separately")
	}
}

func TestEvaluateDemoRejectsEmbeddingFallback(t *testing.T) {
	unavailable := errors.New("fixture unavailable")
	embed := func(_ context.Context, texts []string) ([][]float64, error) {
		if len(texts) == 1 {
			return nil, unavailable
		}
		vectors := make([][]float64, len(texts))
		for n := range vectors {
			vectors[n] = []float64{1}
		}
		return vectors, nil
	}
	if _, err := evaluateDemo(t.Context(), embed); !errors.Is(err, unavailable) {
		t.Fatalf("expected vector evaluation to fail without fallback: %v", err)
	}
}
