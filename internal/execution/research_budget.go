package execution

import (
	"context"
	"errors"
	"strings"

	"researchpilot/internal/repository"
)

// These are protected portions of the existing task limit, not extra tokens.
// Persistence caps the protected portion at half the task limit for small tasks.
const reportBudgetReserve uint32 = 16000
const reviewBudgetReserve uint32 = 6000

type modelBudget struct {
	output uint32
	keep   uint32
}

func budgetForStep(key string) modelBudget {
	switch {
	case strings.HasPrefix(key, "plan-"):
		return modelBudget{1536, reportBudgetReserve}
	case strings.HasPrefix(key, "observe-"):
		return modelBudget{768, reportBudgetReserve}
	case key == "evidence-brief":
		return modelBudget{768, reportBudgetReserve}
	case key == "citation-review":
		return modelBudget{256, 0}
	default:
		return modelBudget{modelOutputLimit, 0}
	}
}

// A rejected reservation has made no provider call. Retry with fewer archived
// excerpt bytes while keeping all instructions and the actual output limit.
func (e execution) generateFittingEvidence(ctx context.Context, config modelConfig, prompt func(int) string, budget modelBudget, sizes ...int) (string, error) {
	var previous string
	for _, size := range sizes {
		candidate := prompt(size)
		if candidate == previous {
			continue
		}
		previous = candidate
		result, err := e.generate(ctx, config, candidate, budget)
		if !errors.Is(err, ErrBudget) {
			return result, err
		}
	}
	return "", ErrBudget
}

func (e execution) preserveReportBudget(ctx context.Context, state *researchState) error {
	state.coverageIncomplete = true
	state.failures += " Collection stopped to preserve the remaining budget for reporting and citation review; disclose unresolved evidence gaps."
	return e.mutate(ctx, func(tx *repository.ExecutionTx, current taskRow) error {
		if err := tx.AbandonTaskAttempts(ctx, e.Tenant, e.Task); err != nil {
			return err
		}
		return eventUpdate(ctx, tx, e.delivery, current, "report_budget_reserved", map[string]string{"reason": "report_and_review_budget_protected"})
	})
}
