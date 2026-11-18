package agent

import "testing"

func TestDependencyPlan(t *testing.T) {
	raw := `{"plan_version":2,"steps":[{"step_id":"report-input","tool":"fetch_page","url":"https://go.dev/doc/","depends_on":["discover"]},{"step_id":"discover","tool":"web_search","query":"Go official"}]}`
	plan, err := ParsePlan(raw, 2)
	if err != nil || plan.Steps[0].ID != "discover" || plan.Steps[1].ID != "report-input" {
		t.Fatal(plan, err)
	}
	for _, raw := range []string{
		`{"steps":[{"step_id":"a","tool":"fetch_page","url":"https://go.dev","depends_on":["a"]}]}`,
		`{"steps":[{"step_id":"a","tool":"fetch_page","url":"https://go.dev","depends_on":["missing"]}]}`,
		`{"steps":[{"step_id":"a","tool":"fetch_page","url":"https://go.dev"},{"step_id":"a","tool":"web_search","query":"go"}]}`,
		`{"steps":[{"step_id":"a","tool":"fetch_page","url":"https://go.dev","depends_on":["b"]},{"step_id":"b","tool":"web_search","query":"go","depends_on":["a"]}]}`,
		`{"plan_version":3,"steps":[{"tool":"web_search","query":"go"}]}`,
	} {
		if _, err := ParsePlan(raw, 2); err == nil {
			t.Fatal("invalid graph accepted", raw)
		}
	}
}
func TestLegacyPlanGetsStableIDs(t *testing.T) {
	p, err := ParsePlan(`{"steps":[{"tool":"fetch_page","url":"https://go.dev"}]}`, 1)
	if err != nil || p.Steps[0].ID != "1" || p.Version != 1 {
		t.Fatal(p, err)
	}
}
