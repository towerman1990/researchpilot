package tools

import "testing"

func TestPlanSchema(t *testing.T) {
	valid := `{"steps":[{"tool":"fetch_page","url":"https://go.dev/doc/"},{"tool":"github_repo","owner":"golang","repo":"go"}]}`
	if steps, err := ParsePlan(valid); err != nil || len(steps) != 2 {
		t.Fatal(steps, err)
	}
	for _, raw := range []string{
		`{"steps":[]}`,
		`{"steps":[{"tool":"shell","query":"rm"}]}`,
		`{"steps":[{"tool":"fetch_page","url":"https://go.dev","command":"ls"}]}`,
		`{"steps":[{"tool":"fetch_page","url":"https://go.dev","query":"extra"}]}`,
		`{"steps":[{"tool":"github_repo","owner":"..","repo":"go"}]}`,
		`{"steps":[{"tool":"web_search","query":"  "}]}`,
		valid + `{}`,
	} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid plan accepted", raw)
		}
	}
}
